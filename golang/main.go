package main

import (
	"context"
	"flag"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")

	flag.Parse()

	// Cancel on SIGINT/SIGTERM so the server drains gracefully on pod termination.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	kConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	// Bound every API call so a hung API server can't hang a request (notably /readyz).
	kConfig.Timeout = 10 * time.Second

	clientset, err := kubernetes.NewForConfig(kConfig)
	if err != nil {
		panic(err)
	}

	version, err := getKubernetesVersion(clientset)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Connected to Kubernetes %s\n", version)

	if err := NewServer(clientset).Start(ctx, *listenAddr); err != nil {
		panic(err)
	}
}

// getKubernetesVersion returns the API server's GitVersion (also a connectivity check).
func getKubernetesVersion(clientset kubernetes.Interface) (string, error) {
	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}

	return version.String(), nil
}
