/*
Copyright 2022.

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
	"flag"
	"fmt"
	"github.com/leemingeer/csi-volume-operator/pkg/utils"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	clientgocache "k8s.io/client-go/tools/cache"
	"k8s.io/sample-controller/pkg/signals"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v3/clientset/versioned"
	volumesnapshotinformers "github.com/kubernetes-csi/external-snapshotter/client/v3/informers/externalversions"
	snapshotrollbackv1beta1 "github.com/leemingeer/csi-volume-operator/api/v1beta1"

	"github.com/leemingeer/csi-volume-operator/pkg/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme           = runtime.NewScheme()
	setupLog         = ctrl.Log.WithName(controllers.Name)
	CSIEndpoint      = flag.String("csi-address", "/run/csi/socket", "The gRPC endpoint for Target CSI Volume.")
	operationTimeout = flag.Duration("timeout", 120*time.Second, "Timeout for waiting for creation or deletion of a volume")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(snapshotrollbackv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func kubeClientInit(stopCh <-chan struct{}) error {

	informersSyncd := make([]clientgocache.InformerSynced, 0)
	config := ctrl.GetConfigOrDie()
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.Wrap(err, "failed to build k8s clientset")
	}

	snapClient, err := volumesnapshot.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error building snapshot clientset: %s", err.Error())
	}
	snapshotInformerFactory := volumesnapshotinformers.NewSharedInformerFactory(snapClient, 0)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	// setup pvc informer
	pvcV1Informer := kubeInformerFactory.Core().V1()
	controllers.PvcInformer = pvcV1Informer
	pvcInformer := pvcV1Informer.PersistentVolumeClaims().Informer()
	informersSyncd = append(informersSyncd, pvcInformer.HasSynced)
	klog.Infof("started PVC informer...")

	snapshotInformers := snapshotInformerFactory.Snapshot().V1beta1()
	controllers.SnapshotInformers = snapshotInformers

	snapInformer := snapshotInformers.VolumeSnapshots().Informer()
	informersSyncd = append(informersSyncd, snapInformer.HasSynced)
	klog.Infof("started VolumeSnapshots informer...")

	snapContentInformer := snapshotInformers.VolumeSnapshotContents().Informer()
	informersSyncd = append(informersSyncd, snapContentInformer.HasSynced)
	klog.Infof("started VolumeSnapshotContents informer...")

	// setup storage class informer
	storagev1Informers := kubeInformerFactory.Storage().V1()
	controllers.SCInformer = storagev1Informers
	scInformer := storagev1Informers.StorageClasses().Informer()
	informersSyncd = append(informersSyncd, scInformer.HasSynced)

	klog.Infof("started storage class informer...")

	go snapshotInformerFactory.Start(stopCh)
	go kubeInformerFactory.Start(stopCh)
	klog.Info("Waiting for informer caches to sync")
	if ok := clientgocache.WaitForCacheSync(stopCh, informersSyncd...); !ok {
		klog.Fatal("failed to wait for all informer caches to be synced")
	}
	klog.Info("all informer caches are synced")

	return nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var port int
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.IntVar(&port, "port", 9443, "The port the manager listens to.")
	flag.IntVar(&controllers.MaxConcurrentReconciles, "max-concurrent-reconciles", 10, "number of reconcile workers.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   port,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9aa3fc06.storage.k8s.io",
		Logger:                 ctrl.Log.WithName(controllers.Name),
	})
	if err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}

	// connect to sock
	grpcClient, err := utils.Connect(*CSIEndpoint)
	if err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}

	err = utils.Probe(grpcClient, *operationTimeout)
	if err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}

	// Autodetect provisioner name
	provisionerName, err := controllers.GetDriverName(grpcClient, *operationTimeout)
	if err != nil {
		klog.Fatalf("Error getting CSI driver name: %s", err)
	}
	setupLog.V(1).Info(fmt.Sprintf("Detected CSI driver %s", provisionerName))

	var stopCh = signals.SetupSignalHandler()
	err = kubeClientInit(stopCh)
	if err != nil {
		setupLog.Error(err, "problem init kubernetes client")
		os.Exit(1)
	}

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = controllers.NewVolumeSnapshotRollBackReconciler(mgr, provisionerName, grpcClient, operationTimeout).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VolumeSnapshotRollBack")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
