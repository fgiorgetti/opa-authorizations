package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/interconnect/opa-controller/internal/cache"
	"github.com/interconnect/opa-controller/internal/configmapwatcher"
	"github.com/interconnect/opa-controller/internal/controller"
	"github.com/interconnect/opa-controller/internal/server"
	"github.com/interconnect/opa-controller/internal/validator"
)

const (
	namespaceFile   = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	defaultAddr     = "0.0.0.0:8080"
	shutdownTimeout = 5 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	nsFlag := flag.String("namespace", "", "namespace of the controller (fallback when not detected)")
	addrFlag := flag.String("addr", "", "HTTP listen address (env HTTP_ADDR, default "+defaultAddr+")")
	flag.Parse()

	namespace := resolveNamespace(*nsFlag)
	if namespace == "" {
		fatal("unable to resolve namespace: set POD_NAMESPACE or --namespace")
	}

	addr := *addrFlag
	if addr == "" {
		addr = os.Getenv("HTTP_ADDR")
	}
	if addr == "" {
		addr = defaultAddr
	}

	cfg, err := loadConfig()
	if err != nil {
		fatal("unable to load kubernetes config", "error", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fatal("unable to create kubernetes client", "error", err)
	}
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		fatal("unable to create dynamic client", "error", err)
	}

	c := cache.New()
	v, err := validator.New()
	if err != nil {
		fatal("unable to create validator", "error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := controller.New(dynClient, v, c)
	go ctrl.Run(ctx)

	cmWatcher := configmapwatcher.New(kubeClient, namespace, v, ctrl.ReevaluateAll)
	go cmWatcher.Run(ctx)

	srv := server.New(c, kubeClient, v, namespace, addr)
	go func() {
		slog.Info("starting http server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "error", err)
		}
	}()

	slog.Info("controller started", "namespace", namespace)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	slog.Info("shutdown signal received", "signal", sig.String())

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown failed", "error", err)
	}
	slog.Info("controller stopped")
}

// resolveNamespace looks at the service account file, then POD_NAMESPACE,
// then the value given by the --namespace flag.
func resolveNamespace(flagValue string) string {
	if data, err := os.ReadFile(namespaceFile); err == nil {
		if ns := strings.TrimSpace(string(data)); ns != "" {
			return ns
		}
	}
	if ns := strings.TrimSpace(os.Getenv("POD_NAMESPACE")); ns != "" {
		return ns
	}
	return strings.TrimSpace(flagValue)
}

// loadConfig tries in-cluster config first, then ~/.kube/config.
func loadConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	slog.Info("in-cluster config not available, falling back to kubeconfig", "reason", err.Error())

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
