/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	"github.com/kubernetes-csi/csi-lib-utils/metrics"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"google.golang.org/grpc"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"

	"github.com/kubernetes-csi/csi-lib-utils/leaderelection"
	csirpc "github.com/kubernetes-csi/csi-lib-utils/rpc"

	clientset "github.com/deepakkinni/volumegroup-controller/client/v1/clientset/versioned"
	volumegroupcheme "github.com/deepakkinni/volumegroup-controller/client/v1/clientset/versioned/scheme"
	informers "github.com/deepakkinni/volumegroup-controller/client/v1/informers/externalversions"

	coreinformers "k8s.io/client-go/informers"
)

const (
	// Default timeout of short CSI calls like GetPluginInfo
	defaultCSITimeout = time.Minute
)

// Command line flags
var (
	kubeconfig   = flag.String("kubeconfig", "", "Absolute path to the kubeconfig file. Required only when running out of cluster.")
	csiAddress   = flag.String("csi-address", "/run/csi/socket", "Address of the CSI driver socket.")
	resyncPeriod = flag.Duration("resync-period", 15*time.Minute, "Resync interval of the controller. Default is 15 minutes")
	showVersion  = flag.Bool("version", false, "Show version.")
	threads      = flag.Int("worker-threads", 10, "Number of worker threads.")
	csiTimeout   = flag.Duration("timeout", defaultCSITimeout, "The timeout for any RPCs to the CSI driver. Default is 1 minute.")

	leaderElection              = flag.Bool("leader-election", false, "Enables leader election.")
	leaderElectionNamespace     = flag.String("leader-election-namespace", "", "The namespace where the leader election resource exists. Defaults to the pod namespace if not set.")
	leaderElectionLeaseDuration = flag.Duration("leader-election-lease-duration", 15*time.Second, "Duration, in seconds, that non-leader candidates will wait to force acquire leadership. Defaults to 15 seconds.")
	leaderElectionRenewDeadline = flag.Duration("leader-election-renew-deadline", 10*time.Second, "Duration, in seconds, that the acting leader will retry refreshing leadership before giving up. Defaults to 10 seconds.")
	leaderElectionRetryPeriod   = flag.Duration("leader-election-retry-period", 5*time.Second, "Duration, in seconds, the LeaderElector clients should wait between tries of actions. Defaults to 5 seconds.")

	kubeAPIQPS   = flag.Float64("kube-api-qps", 5, "QPS to use while communicating with the kubernetes apiserver. Defaults to 5.0.")
	kubeAPIBurst = flag.Int("kube-api-burst", 10, "Burst to use while communicating with the kubernetes apiserver. Defaults to 10.")

	metricsAddress     = flag.String("metrics-address", "", "(deprecated) The TCP network address where the prometheus metrics endpoint will listen (example: `:8080`). The default is empty string, which means metrics endpoint is disabled. Only one of `--metrics-address` and `--http-endpoint` can be set.")
	httpEndpoint       = flag.String("http-endpoint", "", "The TCP network address where the HTTP server for diagnostics, including metrics and leader election health check, will listen (example: `:8080`). The default is empty string, which means the server is disabled. Only one of `--metrics-address` and `--http-endpoint` can be set.")
	metricsPath        = flag.String("metrics-path", "/metrics", "The HTTP path where prometheus metrics will be exposed. Default is `/metrics`.")
	retryIntervalStart = flag.Duration("retry-interval-start", time.Second, "Initial retry interval of failed volumegroup creation or deletion. It doubles with each failure, up to retry-interval-max. Default is 1 second.")
	retryIntervalMax   = flag.Duration("retry-interval-max", 5*time.Minute, "Maximum retry interval of failed volumegroup creation or deletion. Default is 5 minutes.")
)

var (
	version = "unknown"
	prefix  = "external-volumegroup-leader"
)

func main() {
	klog.InitFlags(nil)
	flag.Set("logtostderr", "true")
	flag.Parse()

	if *showVersion {
		fmt.Println(os.Args[0], version)
		os.Exit(0)
	}
	klog.Infof("Version: %s", version)

	// Create the client config. Use kubeconfig if given, otherwise assume in-cluster.
	config, err := buildConfig(*kubeconfig)
	if err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}

	config.QPS = (float32)(*kubeAPIQPS)
	config.Burst = *kubeAPIBurst

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}

	volumeGroupClient, err := clientset.NewForConfig(config)
	if err != nil {
		klog.Errorf("Error building volumegroup clientset: %s", err.Error())
		os.Exit(1)
	}

	factory := informers.NewSharedInformerFactory(volumeGroupClient, *resyncPeriod)
	coreFactory := coreinformers.NewSharedInformerFactory(kubeClient, *resyncPeriod)

	volumegroupcheme.AddToScheme(scheme.Scheme)

	if *metricsAddress != "" && *httpEndpoint != "" {
		klog.Error("only one of `--metrics-address` and `--http-endpoint` can be set.")
		os.Exit(1)
	}
	addr := *metricsAddress
	if addr == "" {
		addr = *httpEndpoint
	}

	// Connect to CSI.
	metricsManager := metrics.NewCSIMetricsManager("" /* driverName */)
	csiConn, err := connection.Connect(
		*csiAddress,
		metricsManager,
		connection.OnConnectionLoss(connection.ExitOnConnectionLoss()))
	if err != nil {
		klog.Errorf("error connecting to CSI driver: %v", err)
		os.Exit(1)
	}

	// Pass a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), *csiTimeout)
	defer cancel()

	// Find driver name
	driverName, err := csirpc.GetDriverName(ctx, csiConn)
	if err != nil {
		klog.Errorf("error getting CSI driver name: %v", err)
		os.Exit(1)
	}

	klog.V(2).Infof("CSI driver name: %q", driverName)

	// Prepare http endpoint for metrics + leader election healthz
	mux := http.NewServeMux()
	if addr != "" {
		metricsManager.RegisterToServer(mux, *metricsPath)
		metricsManager.SetDriverName(driverName)
		go func() {
			klog.Infof("ServeMux listening at %q", addr)
			err := http.ListenAndServe(addr, mux)
			if err != nil {
				klog.Fatalf("Failed to start HTTP server at specified address (%q) and metrics path (%q): %s", addr, *metricsPath, err)
			}
		}()
	}

	// Check it's ready
	if err = csirpc.ProbeForever(csiConn, *csiTimeout); err != nil {
		klog.Errorf("error waiting for CSI driver to be ready: %v", err)
		os.Exit(1)
	}

	// TODO: Re-enable this
	/*supportsVolumeGroup, err := supportsVolumeGroup(ctx, csiConn)
	if err != nil {
		klog.Errorf("error determining if driver supports volumegroup operations: %v", err)
		os.Exit(1)
	}
	if !supportsVolumeGroup {
		klog.Errorf("CSI driver %s does not support VolumeGroup", driverName)
		os.Exit(1)
	}*/

	klog.V(2).Infof("Start NewVolumeGroupSideCarController")

	run := func(context.Context) {
		// run...
		stopCh := make(chan struct{})
		factory.Start(stopCh)
		coreFactory.Start(stopCh)
		// go ctrl.Run(*threads, stopCh)

		// ...until SIGINT
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		close(stopCh)
	}

	if !*leaderElection {
		run(context.TODO())
	} else {
		lockName := fmt.Sprintf("%s-%s", prefix, strings.Replace(driverName, "/", "-", -1))

		leClientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			klog.Fatalf("failed to create leaderelection client: %v", err)
		}
		le := leaderelection.NewLeaderElection(leClientset, lockName, run)
		if *httpEndpoint != "" {
			le.PrepareHealthCheck(mux, leaderelection.DefaultHealthCheckTimeout)
		}

		if *leaderElectionNamespace != "" {
			le.WithNamespace(*leaderElectionNamespace)
		}

		le.WithLeaseDuration(*leaderElectionLeaseDuration)
		le.WithRenewDeadline(*leaderElectionRenewDeadline)
		le.WithRetryPeriod(*leaderElectionRetryPeriod)

		if err := le.Run(); err != nil {
			klog.Fatalf("failed to initialize leader election: %v", err)
		}
	}
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

func supportsVolumeGroup(ctx context.Context, conn *grpc.ClientConn) (bool, error) {
	return true, nil
}
