package controllers

import snapshotrollbackv1beta1 "github.com/leemingeer/csi-volume-operator/api/v1beta1"

type StatusPhase string

const (
	INITStatusPhase        StatusPhase = "Init"
	WAITINGStatusPhase     StatusPhase = "Waiting"
	PreparingStatusPhase   StatusPhase = "Preparing"
	RollbackingStatusPhase StatusPhase = "Rollbacking"
	CompleteStatusPhase    StatusPhase = "Complete"
	ErrorStatusPhase       StatusPhase = "ERROR"
)

const annStorageProvisioner = "volume.beta.kubernetes.io/storage-provisioner"

func NewState(phase StatusPhase, msg string) snapshotrollbackv1beta1.VolumeSnapshotRollBackStatus {
	return snapshotrollbackv1beta1.VolumeSnapshotRollBackStatus{
		Process: string(phase),
		Mesage:  msg,
	}
}
