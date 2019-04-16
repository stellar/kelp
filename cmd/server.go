package cmd

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/spf13/cobra"
	"github.com/stellar/kelp/gui"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Serves the Kelp GUI",
}

type serverInputs struct {
	port *uint16
	dev  *bool
}

func init() {
	options := serverInputs{}
	options.port = serverCmd.Flags().Uint16P("port", "p", 8000, "port on which to serve")
	options.dev = serverCmd.Flags().Bool("dev", false, "run in dev mode for hot-reloading of JS code")

	serverCmd.Run = func(ccmd *cobra.Command, args []string) {
		if env == envDev && *options.dev {
			checkHomeDir()
			runWithYarn(options)
			return
		}

		if env == envDev {
			checkHomeDir()
			generateStaticFiles()
		}

		r := chi.NewRouter()
		r.Use(middleware.RequestID)
		r.Use(middleware.RealIP)
		r.Use(middleware.Logger)
		r.Use(middleware.Recoverer)
		r.Use(middleware.Timeout(60 * time.Second))
		// gui.FS is automatically compiled based on whether this is a local or deployment build
		fileServer(r, "/", gui.FS)

		portString := fmt.Sprintf(":%d", *options.port)
		log.Printf("Serving on HTTP port: %d\n", *options.port)
		e := http.ListenAndServe(portString, r)
		log.Fatal(e)
	}
}

func checkHomeDir() {
	op, e := exec.Command("pwd").Output()
	if e != nil {
		panic(e)
	}
	result := strings.TrimSpace(string(op))

	if !strings.HasSuffix(result, "/kelp") {
		log.Fatalf("need to invoke the '%s' command while in the root 'kelp' directory\n", serverCmd.Use)
	}
}

func runWithYarn(options serverInputs) {
	os.Setenv("PORT", fmt.Sprintf("%d", *options.port))
	log.Printf("Serving on HTTP port: %d\n", *options.port)
	e := exec.Command("yarn", "--cwd", "gui/web", "start").Run()
	if e != nil {
		panic(e)
	}
}

func generateStaticFiles() {
	log.Printf("generating contents of gui/web/build ...\n")

	command := exec.Command("yarn", "--cwd", "gui/web", "build")
	stdout, e := command.StdoutPipe()
	if e != nil {
		panic(e)
	}
	command.Start()

	scanner := bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("\t%s\n", line)
	}

	e = command.Wait()
	if e != nil {
		panic(e)
	}

	log.Printf("... finished generating contents of gui/web/build\n")
	log.Println()
}

// fileServer sets up a http.FileServer handler to serve static files from a http.FileSystem
// example taken from here: https://github.com/go-chi/chi/blob/master/_examples/fileserver/main.go
func fileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}
