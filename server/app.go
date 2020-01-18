package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudsonic/sonic-server/conf"
	"github.com/cloudsonic/sonic-server/log"
	"github.com/cloudsonic/sonic-server/scanner"
	"github.com/cloudsonic/sonic-server/scanner_legacy"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/cors"
)

const Version = "0.2"

type Server struct {
	Importer *scanner_legacy.Importer
	Scanner  *scanner.Scanner
	router   *chi.Mux
}

func New(importer *scanner_legacy.Importer, scanner *scanner.Scanner) *Server {
	a := &Server{Importer: importer, Scanner: scanner}
	if !conf.Sonic.DevDisableBanner {
		showBanner(Version)
	}
	initMimeTypes()
	a.initRoutes()
	if conf.Sonic.DevUseFileScanner {
		log.Info("Using Folder Scanner", "folder", conf.Sonic.MusicFolder)
		a.initScanner()
	} else {
		log.Info("Using iTunes Importer", "xml", conf.Sonic.MusicFolder)
		a.initImporter()
	}
	return a
}

func (a *Server) MountRouter(path string, subRouter http.Handler) {
	log.Info("Mounting API", "path", path)
	a.router.Group(func(r chi.Router) {
		r.Use(middleware.Logger)
		r.Mount(path, subRouter)
	})
}

func (a *Server) Run(addr string) {
	log.Info("CloudSonic server is accepting requests", "address", addr)
	log.Error(http.ListenAndServe(addr, a.router))
}

func (a *Server) initRoutes() {
	r := chi.NewRouter()

	r.Use(cors.Default().Handler)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5, "application/xml", "application/json", "application/javascript"))
	r.Use(middleware.Heartbeat("/ping"))
	r.Use(InjectLogger)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/Jamstash", 302)
	})
	workDir, _ := os.Getwd()
	filesDir := filepath.Join(workDir, "Jamstash-master/dist")
	FileServer(r, "/Jamstash", http.Dir(filesDir))

	a.router = r
}

func (a *Server) initScanner() {
	go func() {
		for {
			select {
			case <-time.After(5 * time.Second):
				err := a.Scanner.RescanAll(false)
				if err != nil {
					log.Error("Error scanning media folder", "folder", conf.Sonic.MusicFolder, err)
				}
			}
		}
	}()
}

func (a *Server) initImporter() {
	go func() {
		first := true
		for {
			select {
			case <-time.After(5 * time.Second):
				if first {
					log.Info("Started iTunes scanner", "xml", conf.Sonic.MusicFolder)
					first = false
				}
				a.Importer.CheckForUpdates(false)
			}
		}
	}()
}

func FileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	})
}

func InjectLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = log.NewContext(r.Context(), "requestId", ctx.Value(middleware.RequestIDKey))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}