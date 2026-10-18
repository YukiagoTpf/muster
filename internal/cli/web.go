package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/YukiagoTpf/muster/internal/platform"
	"github.com/YukiagoTpf/muster/internal/web"
)

// runWeb serves the web page on loopback until Ctrl-C.
func runWeb(sys IO, args []string) int {
	var port int
	var noOpen bool
	if _, ok := parseArgs(sys, "web", args, 0, func(f *flagSet) {
		f.IntVar(&port, "port", 0, "port to listen on (default: a free one)")
		f.BoolVar(&noOpen, "no-open", false, "print the address without opening a browser")
	}); !ok {
		return 2
	}
	ln, err := web.Listen(port)
	if err != nil {
		return fail(sys, err)
	}
	web.Version = Version
	srv, err := web.New(web.Env{Getenv: sys.Getenv, Environ: sys.Environ}, ln.Addr().(*net.TCPAddr).Port)
	if err != nil {
		return fail(sys, err)
	}
	fmt.Fprintf(sys.Out, "Muster is at %s\nCtrl-C stops it.\n", srv.URL())
	if !noOpen {
		if err := platform.OpenURL(srv.URL()); err != nil {
			fmt.Fprintf(sys.Err, "muster: couldn't open a browser (%v); open the address above\n", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	hs := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		hs.Shutdown(shutdown)
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(sys, err)
	}
	return 0
}
