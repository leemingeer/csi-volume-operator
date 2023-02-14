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

package controllers

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/connection"
	"github.com/kubernetes-csi/external-snapshotter/client/v3/apis/volumesnapshot/v1beta1"
	volumesnapshotinformers "github.com/kubernetes-csi/external-snapshotter/client/v3/informers/externalversions/volumesnapshot/v1beta1"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1informers "k8s.io/client-go/informers/core/v1"
	storageinformers "k8s.io/client-go/informers/storage/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	snapshotrollbackv1beta1 "github.com/leemingeer/csi-volume-operator/api/v1beta1"
)

const (
	Name               = "direct-snapshot-rollback"
	FinalizerName      = Name + "/finalizer"
	defaultFSType      = "ext4"
	csiParameterPrefix = "csi.storage.k8s.io/"
	prefixedFsTypeKey  = csiParameterPrefix + "fstype"
)

var (
	ReconcileInterval       int
	PvcInformer             corev1informers.Interface
	SnapshotInformers       volumesnapshotinformers.Interface
	SCInformer              storageinformers.Interface
	PVCSyncMap              sync.Map
	PVCSyncMapMutex         sync.Mutex
	MaxConcurrentReconciles int
)

func init() {
	flag.IntVar(&ReconcileInterval, "interval", 15, "The interval the reconciler works.")
}

func NewVolumeSnapshotRollBackReconciler(mgr manager.Manager, driverName string, grpcClient *grpc.ClientConn, operationTimeout *time.Duration) *VolumeSnapshotRollBackReconciler {
	return &VolumeSnapshotRollBackReconciler{
		Client:            mgr.GetClient(),
		driverName:        driverName,
		Scheme:            mgr.GetScheme(),
		PvcInformer:       PvcInformer,
		SnapshotInformers: SnapshotInformers,
		SCInformer:        SCInformer,
		CSIClient:         csi.NewControllerClient(grpcClient),
		Timeout:           *operationTimeout,
		Recorder:          mgr.GetEventRecorderFor(Name),
	}

}

// VolumeSnapshotRollBackReconciler reconciles a VolumeSnapshotRollBack object
type VolumeSnapshotRollBackReconciler struct {
	client.Client
	driverName        string
	Scheme            *runtime.Scheme
	PvcInformer       corev1informers.Interface
	SnapshotInformers volumesnapshotinformers.Interface
	SCInformer        storageinformers.Interface
	CSIClient         csi.ControllerClient
	Recorder          record.EventRecorder
	Timeout           time.Duration
}

//+kubebuilder:rbac:groups=snapshotrollback.storage.k8s.io,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=snapshotrollback.storage.k8s.io,resources=volumesnapshotrollbacks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=snapshotrollback.storage.k8s.io,resources=volumesnapshotrollbacks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=snapshotrollback.storage.k8s.io,resources=volumesnapshotrollbacks/finalizers,verbs=update
//+kubebuilder:rbac:groups=snapshotrollback.storage.k8s.io,resources=volumesnapshotrollbacks,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VolumeSnapshotRollBack object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *VolumeSnapshotRollBackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	Log := log.FromContext(ctx)
	Log.V(0).Info(fmt.Sprintf("Reconcile %s begin", req.NamespacedName))
	requeueAfter := time.Duration(ReconcileInterval) * time.Second
	var err error
	v := new(snapshotrollbackv1beta1.VolumeSnapshotRollBack)
	if err := r.Get(ctx, req.NamespacedName, v); err != nil {
		Log.V(2).Info("Unable to fetch any volumesnapshotrollback, do Nothing")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if v.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(v, FinalizerName) {
			controllerutil.AddFinalizer(v, FinalizerName)
			if err := r.Update(ctx, v); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(v, FinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(v); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(v, FinalizerName)
			if err := r.Update(ctx, v); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	if v.Status.Process == string(CompleteStatusPhase) {
		Log.V(4).Info(fmt.Sprintf("%v already in %v state!", v.Name, v.Status.Process))
		return ctrl.Result{}, nil
	}

	Log.V(4).Info("Reconciling pvc with volumesnapshot", "pvc name ", v.Spec.PersistentVolumeClaimName, "volume snapshot", v.Spec.VolumeSnapshot)

	pvc := &corev1.PersistentVolumeClaim{}
	if pvc, err = r.PvcInformer.PersistentVolumeClaims().Lister().PersistentVolumeClaims(req.Namespace).Get(v.Spec.PersistentVolumeClaimName); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, wrapError("Get pvc failed", err)
		}
		r.EmitErrorEvent(v, err, fmt.Sprintf("PVC not found: %v", req.NamespacedName), err.Error())
		updateErr := r.UpdateStatus(ctx, v, ErrorStatusPhase, fmt.Sprintf("PVC not found: %v!", req.NamespacedName))
		if updateErr != nil {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	if found, should := r.ShouldProvision(pvc); !found {
		Log.V(0).Info(fmt.Sprintf("PVC(%v) is not in bound state, current state is: %v", v.Spec.PersistentVolumeClaimName, pvc.Status.Phase))
		r.EmitErrorEvent(v, errors.New("should provision failed"), fmt.Sprintf("PVC(%v) status not in bound state", v.Spec.PersistentVolumeClaimName), "")
		r.UpdateStatus(ctx, v, INITStatusPhase, fmt.Sprintf("Init reconcile(%v) process", req.NamespacedName))
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	} else if !should {
		return ctrl.Result{}, nil
	}

	PVCSyncMapKey := req.NamespacedName.Namespace + v.Spec.PersistentVolumeClaimName
	PVCSyncMapMutex.Lock()
	if _, exists := PVCSyncMap.Load(PVCSyncMapKey); exists {
		PVCSyncMapMutex.Unlock()
		Log.V(0).Info(fmt.Sprintf("PVC is processing, wait for the next loop, namespace: %v, name: %v", v.Namespace, v.Name))
		r.EmitErrorEvent(v, err, fmt.Sprintf("PVC is processing, wait for the next loop, namespace: %v, name: %v", v.Namespace, v.Name), "")
		r.UpdateStatus(ctx, v, WAITINGStatusPhase, fmt.Sprintf("PVC is processing, wait for the next loop, namespace: %v, name: %v", v.Namespace, v.Name))
		return ctrl.Result{Requeue: true}, nil
	}
	PVCSyncMap.Store(PVCSyncMapKey, v)
	PVCSyncMapMutex.Unlock()

	defer func() {
		PVCSyncMapMutex.Lock()
		PVCSyncMap.Delete(PVCSyncMapKey)
		PVCSyncMapMutex.Unlock()
	}()

	var sourceSnapshot *v1beta1.VolumeSnapshot
	if sourceSnapshot, err = r.SnapshotInformers.VolumeSnapshots().Lister().VolumeSnapshots(pvc.Namespace).Get(v.Spec.VolumeSnapshot); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, wrapError("Get volume snapshot failed", err)
		}
		r.EmitErrorEvent(v, err, fmt.Sprintf("Volume snapshot(%v) not found", v.Spec.VolumeSnapshot), err.Error())
		updateErr := r.UpdateStatus(ctx, v, ErrorStatusPhase, fmt.Sprintf("Volume snapshot(%v) not found", v.Spec.VolumeSnapshot))
		if updateErr != nil {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	r.UpdateStatus(ctx, v, PreparingStatusPhase, "Creating snapshot rollback request")

	if sourceSnapshot.Status == nil || *sourceSnapshot.Status.ReadyToUse != true {
		err := fmt.Errorf("Snapshot %s is not ready", v.Spec.VolumeSnapshot)
		Log.V(0).Info("Snapshot is not ready", "volumeSnapshot", v.Spec.VolumeSnapshot)
		r.EmitErrorEvent(v, err, fmt.Sprintf("Volume snapshot(%v) status not ready to use", v.Spec.VolumeSnapshot), err.Error())
		return ctrl.Result{}, wrapError("Volume snapshot status not ready to use", err)
	}

	pvcSize := pvc.Status.Capacity[corev1.ResourceStorage]
	if !pvcSize.Equal(*sourceSnapshot.Status.RestoreSize) {
		err := fmt.Errorf("%s is not allow rollback", v.Spec.VolumeSnapshot)
		Log.Error(err, "snapshot not allow rollback, size is not equal to pvc", "volumeSnapshot", v.Spec.VolumeSnapshot)
		r.EmitErrorEvent(v, err, "snapshot not allow rollback", fmt.Sprintf("Volume snapshot(%v) size is not equal pvc size", v.Spec.VolumeSnapshot))
		updateErr := r.UpdateStatus(ctx, v, ErrorStatusPhase, fmt.Sprintf("Volume snapshot(%v) size is not equal pvc size", v.Spec.VolumeSnapshot))
		if updateErr != nil {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	snapshotContentName := *sourceSnapshot.Status.BoundVolumeSnapshotContentName
	scName := *pvc.Spec.StorageClassName
	createVolReq, err := r.buildCreateVolumeRequest(snapshotContentName, scName, pvc)
	if err != nil {
		Log.Error(err, "buildCreateVolumeRequest error", "objName", req.NamespacedName)
		r.EmitErrorEvent(v, err, fmt.Sprintf("buildCreateVolumeRequest error in %s/%s", v.Namespace, v.Name), err.Error())
		r.UpdateStatus(ctx, v, ErrorStatusPhase, fmt.Sprintf("buildCreateVolumeRequest error %v in %s/%s", err, v.Namespace, v.Name))
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}
	Log.V(4).Info(fmt.Sprintf("CreateVolumeRequest info: %+v", *createVolReq))

	r.UpdateStatus(ctx, v, RollbackingStatusPhase, "Snapshot direct rollback in progress")

	ctx, cancel := context.WithTimeout(context.Background(), r.Timeout)
	defer cancel()
	_, err = r.CSIClient.CreateVolume(ctx, createVolReq)
	if err != nil {
		Log.Error(err, "call create volume err ", "objName", req.NamespacedName)
		r.UpdateStatus(ctx, v, ErrorStatusPhase, fmt.Sprintf("Call create volume with req(%v) failed!", *createVolReq))
		r.EmitErrorEvent(v, err, fmt.Sprint("Call create volume failed!"), err.Error())
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	err = r.UpdateStatus(ctx, v, CompleteStatusPhase, fmt.Sprintf("Complete snapshot direct rollback for pvc: %v", v.Spec.PersistentVolumeClaimName))
	if err != nil {
		Log.Error(err, "createVolume success, but update status error", "objName", req.NamespacedName)
		r.EmitErrorEvent(v, err, fmt.Sprintf("Complete snapshot direct rollback for pvc: %v Failed!", v.Spec.PersistentVolumeClaimName), err.Error())
		return ctrl.Result{}, err
	}

	Log.V(1).Info(fmt.Sprintf("Reconcile %s success", req.NamespacedName))
	return ctrl.Result{}, nil
}

func (r *VolumeSnapshotRollBackReconciler) buildCreateVolumeRequest(snapshotContentName, scName string,
	pvc *corev1.PersistentVolumeClaim) (*csi.CreateVolumeRequest, error) {

	volumeSnapContent, err := r.SnapshotInformers.VolumeSnapshotContents().Lister().Get(snapshotContentName)
	if err != nil {
		return nil, wrapError("Unable to get volume snapshot content", err)
	}

	sc, err := r.SCInformer.StorageClasses().Lister().Get(scName)
	if err != nil {
		return nil, wrapError("Unable to get storage class", err)
	}

	if sc.Parameters == nil {
		sc.Parameters = make(map[string]string)
	}

	snapshotrollbackParameter := sc.Parameters
	snapshotrollbackParameter["SnapPathHandler"] = *volumeSnapContent.Status.SnapshotHandle
	snapshotrollbackParameter["VolumeHandle"] = *volumeSnapContent.Spec.Source.VolumeHandle
	snapshotrollbackParameter["directRollBack"] = "true"

	fsType := defaultFSType
	for k, v := range sc.Parameters {
		if strings.ToLower(k) == "fstype" || k == prefixedFsTypeKey {
			fsType = v
		}
	}

	// Get access mode
	volumeCaps := make([]*csi.VolumeCapability, 0)
	for _, pvcAccessMode := range pvc.Spec.AccessModes {
		volumeCaps = append(volumeCaps, getVolumeCapability(pvc, sc, pvcAccessMode, fsType))
	}

	capacity := pvc.Spec.Resources.Requests[corev1.ResourceName(corev1.ResourceStorage)]

	createVolReq := csi.CreateVolumeRequest{
		Name:               pvc.Spec.VolumeName,
		VolumeCapabilities: volumeCaps,
		Parameters:         snapshotrollbackParameter,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: capacity.Value(),
		},
	}
	return &createVolReq, nil
}

func (r *VolumeSnapshotRollBackReconciler) UpdateStatus(ctx context.Context, v *snapshotrollbackv1beta1.VolumeSnapshotRollBack, status StatusPhase, msg string) error {
	Log := log.FromContext(ctx)
	new := new(snapshotrollbackv1beta1.VolumeSnapshotRollBack)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		namespaceName := types.NamespacedName{
			Namespace: v.Namespace,
			Name:      v.Name,
		}
		err := r.Get(ctx, namespaceName, new)
		if err != nil {
			return err
		}

		new.Status = NewState(status, msg)
		err = r.Client.Status().Update(ctx, new)
		return err
	})

	if err != nil {
		Log.Error(err, "updating status error", "status",
			status, "namespace", v.Namespace, "name", v.Name)
		return err
	}
	return nil
}

func (r *VolumeSnapshotRollBackReconciler) EmitErrorEvent(obj runtime.Object, err error, reason, message string, args ...interface{}) {
	// ignore nil errors and conflict issues
	if err == nil || apierrors.IsConflict(err) {
		return
	}
	r.Recorder.Eventf(obj, corev1.EventTypeWarning, reason, message, args...)
}

func (r *VolumeSnapshotRollBackReconciler) deleteExternalResources(v *snapshotrollbackv1beta1.VolumeSnapshotRollBack) error {
	//
	// delete any external resources associated with the snapshotrollbackv1beta1.VolumeSnapshotRollBack
	//
	// Ensure that delete implementation is idempotent and safe to invoke
	// multiple times for same object.
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VolumeSnapshotRollBackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&snapshotrollbackv1beta1.VolumeSnapshotRollBack{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: MaxConcurrentReconciles}).
		Complete(r)
}

func (r *VolumeSnapshotRollBackReconciler) ShouldProvision(claim *corev1.PersistentVolumeClaim) (bool, bool) {
	if provisioner, found := claim.Annotations[annStorageProvisioner]; found && claim.Status.Phase == "Bound" {
		if provisioner == r.driverName {
			// Either CSI volume is requested or in-tree volume is migrated to CSI in PV controller
			// and therefore PVC has CSI annotation.
			return true, true
		}
		return true, false
	}
	// Non-migrated in-tree volume is requested.
	return false, false
}

func GetDriverName(conn *grpc.ClientConn, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return connection.GetDriverName(ctx, conn)
}

func getVolumeCapability(pvc *corev1.PersistentVolumeClaim, sc *storagev1.StorageClass,
	pvcAccessMode corev1.PersistentVolumeAccessMode,
	fsType string,
) *csi.VolumeCapability {
	return &csi.VolumeCapability{
		AccessType: getAccessTypeMount(fsType, sc.MountOptions),
		AccessMode: getAccessMode(pvcAccessMode),
	}

}

func getAccessMode(pvcAccessMode corev1.PersistentVolumeAccessMode) *csi.VolumeCapability_AccessMode {
	switch pvcAccessMode {
	case corev1.ReadWriteOnce:
		return &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		}
	case corev1.ReadWriteMany:
		return &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
		}
	case corev1.ReadOnlyMany:
		return &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		}
	default:
		return nil
	}
}

func getAccessTypeMount(fsType string, mountFlags []string) *csi.VolumeCapability_Mount {
	return &csi.VolumeCapability_Mount{
		Mount: &csi.VolumeCapability_MountVolume{
			FsType:     fsType,
			MountFlags: mountFlags,
		},
	}
}
