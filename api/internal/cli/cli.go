// Package cli implements the tracker-api subcommands.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"tracker/api/internal/app"
	"tracker/api/internal/auth"
	"tracker/api/internal/config"
	"tracker/api/internal/db"
	"tracker/api/internal/server"
	"tracker/api/internal/store"
)

const usage = `usage:
  tracker-api serve                         run the HTTP API (default)
  tracker-api user create --email EMAIL     create the user; prompts for the password
  tracker-api user reset-password --email EMAIL
                                            set a new password; revokes all of the user's sessions
  tracker-api sessions revoke-all           revoke every session of every user
`

// Run dispatches args (without the program name) and returns an exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	var err error
	switch {
	case args[0] == "serve" && len(args) == 1:
		err = serve()
	case len(args) >= 2 && args[0] == "user" && args[1] == "create":
		err = userCreate(args[2:], stdout)
	case len(args) >= 2 && args[0] == "user" && args[1] == "reset-password":
		err = userResetPassword(args[2:], stdout)
	case len(args) == 2 && args[0] == "sessions" && args[1] == "revoke-all":
		err = sessionsRevokeAll(stdout)
	case args[0] == "help" || args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	hasher, err := auth.NewHasher(auth.DefaultParams, 4)
	if err != nil {
		return err
	}
	srv := server.New(server.Options{
		Store:        store.New(d, time.Now),
		Hasher:       hasher,
		Logger:       logger,
		TrustedProxy: cfg.TrustedProxy,
	})
	go srv.RunJanitor(ctx)

	hs := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr, "app", app.Name)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := hs.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}

func openStore(ctx context.Context) (*store.Store, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	return store.New(d, time.Now), func() { _ = d.Close() }, nil
}

func emailFlag(name string, args []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	email := fs.String("email", "", "account email")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() != 0 {
		return "", errors.New("unexpected arguments")
	}
	e := strings.ToLower(strings.TrimSpace(*email))
	if e == "" || len(e) > 254 || !strings.Contains(e, "@") || strings.ContainsAny(e, " \t\r\n") {
		return "", errors.New("--email must be a valid email address")
	}
	return e, nil
}

// readNewPassword prompts twice on the terminal without echo.
func readNewPassword(out io.Writer) (string, error) {
	fd := int(os.Stdin.Fd()) // #nosec G115 -- file descriptors fit in int
	if !term.IsTerminal(fd) {
		return "", errors.New("the password must be entered on an interactive terminal (use docker compose exec -it / run -it)")
	}
	read := func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(out)
		return string(b), err
	}
	pw, err := read("New password (min 12 characters): ")
	if err != nil {
		return "", err
	}
	if err := auth.CheckPolicy(pw); err != nil {
		return "", err
	}
	again, err := read("Repeat password: ")
	if err != nil {
		return "", err
	}
	if pw != again {
		return "", errors.New("passwords do not match")
	}
	return pw, nil
}

func userCreate(args []string, out io.Writer) error {
	email, err := emailFlag("user create", args)
	if err != nil {
		return err
	}
	pw, err := readNewPassword(out)
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, closeFn, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	hasher, err := auth.NewHasher(auth.DefaultParams, 1)
	if err != nil {
		return err
	}
	hash, err := hasher.Hash(pw)
	if err != nil {
		return err
	}
	u, err := st.CreateUser(ctx, email, hash)
	if errors.Is(err, store.ErrConflict) {
		return errors.New("a user with that email already exists")
	}
	if err != nil {
		return err
	}
	_ = st.RecordAuthEvent(ctx, u.ID, store.EventUserCreated, "cli")
	fmt.Fprintf(out, "Created user %s\n", email)
	return nil
}

func userResetPassword(args []string, out io.Writer) error {
	email, err := emailFlag("user reset-password", args)
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, closeFn, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	u, err := st.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		return errors.New("no user with that email")
	}
	if err != nil {
		return err
	}
	pw, err := readNewPassword(out)
	if err != nil {
		return err
	}
	hasher, err := auth.NewHasher(auth.DefaultParams, 1)
	if err != nil {
		return err
	}
	hash, err := hasher.Hash(pw)
	if err != nil {
		return err
	}
	if err := st.SetPassword(ctx, u.ID, hash, ""); err != nil {
		return err
	}
	_ = st.RecordAuthEvent(ctx, u.ID, store.EventPasswordReset, "cli")
	fmt.Fprintln(out, "Password updated; all of this user's sessions were revoked.")
	return nil
}

func sessionsRevokeAll(out io.Writer) error {
	ctx := context.Background()
	st, closeFn, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	fmt.Fprint(out, "Revoke every session of every user? Type 'yes' to confirm: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) != "yes" {
		return errors.New("aborted")
	}
	n, err := st.RevokeAllSessions(ctx)
	if err != nil {
		return err
	}
	_ = st.RecordAuthEvent(ctx, "", store.EventRevokeAllCLI, "cli")
	fmt.Fprintf(out, "Revoked %d session(s).\n", n)
	return nil
}
