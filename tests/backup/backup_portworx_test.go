package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/pborman/uuid"
	api "github.com/portworx/px-backup-api/pkg/apis/v1"
	"github.com/portworx/sched-ops/k8s/core"
	"github.com/portworx/sched-ops/k8s/storage"
	"github.com/portworx/torpedo/drivers/backup"
	"github.com/portworx/torpedo/drivers/backup/portworx"
	"github.com/portworx/torpedo/drivers/scheduler"
	"github.com/portworx/torpedo/drivers/scheduler/k8s"
	"github.com/portworx/torpedo/drivers/volume"
	"github.com/portworx/torpedo/pkg/log"
	v1 "k8s.io/api/core/v1"
	storageApi "k8s.io/api/storage/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/portworx/torpedo/tests"
)

// This test case creates a backup location with encryption
var _ = Describe("{BackupLocationWithEncryptionKey}", func() {
	var contexts []*scheduler.Context
	var appContexts []*scheduler.Context
	backupLocationMap := make(map[string]string)
	var bkpNamespaces []string
	var backupLocationName string
	var CloudCredUID string
	var clusterUid string
	var cloudCredName string
	var restoreName string
	var backupName string
	var clusterStatus api.ClusterInfo_StatusInfo_Status
	JustBeforeEach(func() {
		StartTorpedoTest("BackupLocationWithEncryptionKey", "Creating Backup Location with Encryption Keys", nil, 79918)
	})
	It("Creating cloud account and backup location", func() {
		log.InfoD("Creating cloud account and backup location")
		providers := getProviders()
		cloudCredName = fmt.Sprintf("%s-%s-%v", "cred", providers[0], time.Now().Unix())
		backupLocationName = fmt.Sprintf("autogenerated-backup-location-%v", time.Now().Unix())
		CloudCredUID = uuid.New()
		BackupLocationUID = uuid.New()
		encryptionKey := generateEncryptionKey()
		backupLocationMap[BackupLocationUID] = backupLocationName
		ctx, err := backup.GetAdminCtxFromSecret()
		log.FailOnError(err, "Fetching px-central-admin ctx")
		err = CreateCloudCredential(providers[0], cloudCredName, CloudCredUID, orgID, ctx)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying creation of cloud credential named [%s] for org [%s] with [%s] as provider", cloudCredName, orgID, providers[0]))
		err = CreateBackupLocation(providers[0], backupLocationName, BackupLocationUID, cloudCredName, CloudCredUID, getGlobalBucketName(providers[0]), orgID, encryptionKey)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Creating backup location %s", backupLocationName))
		log.InfoD("Deploy applications")
		contexts = make([]*scheduler.Context, 0)
		for i := 0; i < Inst().GlobalScaleFactor; i++ {
			taskName := fmt.Sprintf("%s-%d", taskNamePrefix, i)
			appContexts = ScheduleApplications(taskName)
			contexts = append(contexts, appContexts...)
			for _, ctx := range appContexts {
				ctx.ReadinessTimeout = appReadinessTimeout
				namespace := GetAppNamespace(ctx, taskName)
				bkpNamespaces = append(bkpNamespaces, namespace)
			}
		}

		Step("Register clusters for backup", func() {
			log.InfoD("Register clusters for backup")
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			err = CreateSourceAndDestClusters(orgID, "", "", ctx)
			dash.VerifyFatal(err, nil, "Creating source and destination cluster")
			clusterStatus, err = Inst().Backup.GetClusterStatus(orgID, SourceClusterName, ctx)
			log.FailOnError(err, fmt.Sprintf("Fetching [%s] cluster status", SourceClusterName))
			dash.VerifyFatal(clusterStatus, api.ClusterInfo_StatusInfo_Online, fmt.Sprintf("Verifying if [%s] cluster is online", SourceClusterName))
			clusterUid, err = Inst().Backup.GetClusterUID(ctx, orgID, SourceClusterName)
			dash.VerifyFatal(err, nil, fmt.Sprintf("Fetching [%s] cluster uid", SourceClusterName))
		})

		Step("Taking backup of applications", func() {
			log.InfoD("Taking backup of applications")
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			for _, namespace := range bkpNamespaces {
				backupName = fmt.Sprintf("%s-%s-%v", BackupNamePrefix, namespace, time.Now().Unix())
				err = CreateBackup(backupName, SourceClusterName, backupLocationName, BackupLocationUID, []string{namespace},
					nil, orgID, clusterUid, "", "", "", "", ctx)
				dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying backup %s creation", backupName))
			}
		})

		Step("Restoring the backed up application", func() {
			log.InfoD("Restoring the backed up application")
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			restoreName = fmt.Sprintf("%s-%s-%v", restoreNamePrefix, backupName, time.Now().Unix())
			err = CreateRestore(restoreName, backupName, nil, destinationClusterName, orgID, ctx, make(map[string]string))
			log.FailOnError(err, "%s restore failed", restoreName)
		})
	})
	JustAfterEach(func() {
		defer EndPxBackupTorpedoTest(contexts)
		log.Infof("Deleting backup, restore and backup location, cloud account")
		ctx, err := backup.GetAdminCtxFromSecret()
		log.FailOnError(err, "Fetching px-central-admin ctx")
		err = DeleteRestore(restoreName, orgID, ctx)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Deleting Restore %s", restoreName))
		backupUID, err := Inst().Backup.GetBackupUID(ctx, backupName, orgID)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Getting backup UID for backup %s", backupName))
		_, err = DeleteBackup(backupName, backupUID, orgID, ctx)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Deleting backup %s", backupName))
		CleanupCloudSettingsAndClusters(backupLocationMap, cloudCredName, CloudCredUID, ctx)
	})

})

// Change replica while restoring backup through StorageClass Mapping.
var _ = Describe("{ReplicaChangeWhileRestore}", func() {
	namespaceMapping := make(map[string]string)
	storageClassMapping := make(map[string]string)
	var contexts []*scheduler.Context
	CloudCredUIDMap := make(map[string]string)
	var appContexts []*scheduler.Context
	var backupLocation string
	var backupLocationUID string
	var cloudCredUID string
	backupLocationMap := make(map[string]string)
	var bkpNamespaces []string
	var clusterUid string
	var cloudCredName string
	var clusterStatus api.ClusterInfo_StatusInfo_Status
	var backupName string
	var restoreName string
	bkpNamespaces = make([]string, 0)
	labelSelectors := make(map[string]string)
	params := make(map[string]string)
	var backupNames []string
	var scName string

	JustBeforeEach(func() {
		StartTorpedoTest("ReplicaChangeWhileRestore", "Change replica while restoring backup", nil, 58065)
		log.InfoD("Deploy applications")
		contexts = make([]*scheduler.Context, 0)
		for i := 0; i < Inst().GlobalScaleFactor; i++ {
			taskName := fmt.Sprintf("%s-%d", taskNamePrefix, i)
			appContexts = ScheduleApplications(taskName)
			contexts = append(contexts, appContexts...)
			for _, ctx := range appContexts {
				ctx.ReadinessTimeout = appReadinessTimeout
				namespace := GetAppNamespace(ctx, taskName)
				bkpNamespaces = append(bkpNamespaces, namespace)
			}
		}
	})
	It("Change replica while restoring backup", func() {
		Step("Validate applications", func() {
			ValidateApplications(contexts)
		})

		Step("Creating cloud credentials", func() {
			log.InfoD("Creating cloud credentials")
			providers := getProviders()
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			for _, provider := range providers {
				cloudCredName = fmt.Sprintf("%s-%s-%v", "cred", provider, time.Now().Unix())
				cloudCredUID = uuid.New()
				CloudCredUIDMap[cloudCredUID] = cloudCredName
				err := CreateCloudCredential(provider, cloudCredName, cloudCredUID, orgID, ctx)
				dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying creation of cloud credential named [%s] for org [%s] with [%s] as provider", cloudCredName, orgID, provider))
			}
		})

		Step("Register cluster for backup", func() {
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			err = CreateSourceAndDestClusters(orgID, "", "", ctx)
			dash.VerifyFatal(err, nil, "Creating source and destination cluster")
			clusterStatus, err = Inst().Backup.GetClusterStatus(orgID, SourceClusterName, ctx)
			log.FailOnError(err, fmt.Sprintf("Fetching [%s] cluster status", SourceClusterName))
			dash.VerifyFatal(clusterStatus, api.ClusterInfo_StatusInfo_Online, fmt.Sprintf("Verifying if [%s] cluster is online", SourceClusterName))
			clusterUid, err = Inst().Backup.GetClusterUID(ctx, orgID, SourceClusterName)
			dash.VerifyFatal(err, nil, fmt.Sprintf("Fetching [%s] cluster uid", SourceClusterName))
		})

		Step("Creating backup location", func() {
			log.InfoD("Creating backup location")
			providers := getProviders()
			for _, provider := range providers {
				backupLocation = fmt.Sprintf("autogenerated-backup-location-%v", time.Now().Unix())
				backupLocationUID = uuid.New()
				backupLocationMap[backupLocationUID] = backupLocation
				err := CreateBackupLocation(provider, backupLocation, backupLocationUID, cloudCredName, cloudCredUID,
					getGlobalBucketName(provider), orgID, "")
				dash.VerifyFatal(err, nil, fmt.Sprintf("Creating backup location %s", backupLocation))
			}
		})
		Step("Taking backup of applications", func() {
			log.InfoD("Taking backup of applications")
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			for _, namespace := range bkpNamespaces {
				backupName = fmt.Sprintf("%s-%s-%v", BackupNamePrefix, namespace, time.Now().Unix())
				err = CreateBackup(backupName, SourceClusterName, backupLocation, backupLocationUID, []string{namespace}, labelSelectors, orgID, clusterUid, "", "", "", "", ctx)
				dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying backup %s creation with custom resources", backupName))
				backupNames = append(backupNames, backupName)
			}
		})
		Step("Create new storage class for restore", func() {
			log.InfoD("Create new storage class for restore")
			scName = fmt.Sprintf("replica-sc-%v", time.Now().Unix())
			params["repl"] = "2"
			k8sStorage := storage.Instance()
			v1obj := metaV1.ObjectMeta{
				Name: scName,
			}
			reclaimPolicyDelete := v1.PersistentVolumeReclaimDelete
			bindMode := storageApi.VolumeBindingImmediate
			scObj := storageApi.StorageClass{
				ObjectMeta:        v1obj,
				Provisioner:       k8s.CsiProvisioner,
				Parameters:        params,
				ReclaimPolicy:     &reclaimPolicyDelete,
				VolumeBindingMode: &bindMode,
			}
			_, err := k8sStorage.CreateStorageClass(&scObj)
			dash.VerifyFatal(err, nil, "Verifying creation of new storage class")
		})

		Step("Restoring the backed up application", func() {
			log.InfoD("Restoring the backed up application")
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			for _, namespace := range bkpNamespaces {
				for _, backupName := range backupNames {
					restoreName = fmt.Sprintf("%s-%s-%v", restoreNamePrefix, backupName, time.Now().Unix())
					pvcs, err := core.Instance().GetPersistentVolumeClaims(namespace, labelSelectors)
					dash.VerifyFatal(err, nil, fmt.Sprintf("Getting all PVCs from namespace [%s]. Total PVCs - %d", namespace, len(pvcs.Items)))
					singlePvc := pvcs.Items[0]
					sourceScName, err := core.Instance().GetStorageClassForPVC(&singlePvc)
					dash.VerifyFatal(err, nil, fmt.Sprintf("Getting SC from PVC - %s", singlePvc.GetName()))
					storageClassMapping[sourceScName.Name] = scName
					restoredNameSpace := fmt.Sprintf("%s-%s", namespace, "restored")
					namespaceMapping[namespace] = restoredNameSpace
					err = CreateRestore(restoreName, backupName, namespaceMapping, SourceClusterName, orgID, ctx, storageClassMapping)
					dash.VerifyFatal(err, nil, fmt.Sprintf("Restoring with custom Storage Class Mapping - %v", namespaceMapping))
				}
			}
		})
		Step("Validate applications", func() {
			ValidateApplications(contexts)
		})
	})
	JustAfterEach(func() {
		defer EndPxBackupTorpedoTest(contexts)
		ctx, err := backup.GetAdminCtxFromSecret()
		log.FailOnError(err, "Fetching px-central-admin ctx")
		log.InfoD("Deleting the deployed apps after the testcase")
		opts := make(map[string]bool)
		opts[SkipClusterScopedObjects] = true
		ValidateAndDestroy(contexts, opts)

		backupUID, err := Inst().Backup.GetBackupUID(ctx, backupName, orgID)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Getting backup UID for backup %s", backupName))
		_, err = DeleteBackup(backupName, backupUID, orgID, ctx)
		dash.VerifySafely(err, nil, fmt.Sprintf("Deleting backup [%s]", backupName))
		err = DeleteRestore(restoreName, orgID, ctx)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Deleting Restore [%s]", restoreName))
		CleanupCloudSettingsAndClusters(backupLocationMap, cloudCredName, cloudCredUID, ctx)
	})
})

// This testcase verifies resize after the volume is restored from a backup
var _ = Describe("{ResizeOnRestoredVolume}", func() {
	var (
		appList          = Inst().AppList
		contexts         []*scheduler.Context
		preRuleNameList  []string
		postRuleNameList []string
		appContexts      []*scheduler.Context
		bkpNamespaces    []string
		clusterUid       string
		clusterStatus    api.ClusterInfo_StatusInfo_Status
		restoreName      string
		namespaceMapping map[string]string
		credName         string
	)
	labelSelectors := make(map[string]string)
	CloudCredUIDMap := make(map[string]string)
	BackupLocationMap := make(map[string]string)
	var backupLocation string
	contexts = make([]*scheduler.Context, 0)
	bkpNamespaces = make([]string, 0)
	backupNamespaceMap := make(map[string]string)

	JustBeforeEach(func() {
		StartTorpedoTest("ResizeOnRestoredVolume", "Resize after the volume is restored from a backup", nil, 58064)
		log.InfoD("Verifying if the pre/post rules for the required apps are present in the list or not")
		for i := 0; i < len(appList); i++ {
			if Contains(postRuleApp, appList[i]) {
				if _, ok := portworx.AppParameters[appList[i]]["post_action_list"]; ok {
					dash.VerifyFatal(ok, true, "Post Rule details mentioned for the apps")
				}
			}
			if Contains(preRuleApp, appList[i]) {
				if _, ok := portworx.AppParameters[appList[i]]["pre_action_list"]; ok {
					dash.VerifyFatal(ok, true, "Pre Rule details mentioned for the apps")
				}
			}
		}
		log.InfoD("Deploy applications")
		contexts = make([]*scheduler.Context, 0)
		for i := 0; i < Inst().GlobalScaleFactor; i++ {
			taskName := fmt.Sprintf("%s-%d", taskNamePrefix, i)
			appContexts = ScheduleApplications(taskName)
			contexts = append(contexts, appContexts...)
			for _, ctx := range appContexts {
				ctx.ReadinessTimeout = appReadinessTimeout
				namespace := GetAppNamespace(ctx, taskName)
				bkpNamespaces = append(bkpNamespaces, namespace)
			}
		}
	})
	It("Resize after the volume is restored from a backup", func() {
		providers := getProviders()
		Step("Validate applications", func() {
			ValidateApplications(contexts)
		})

		Step("Creating rules for backup", func() {
			log.InfoD("Creating pre rule for deployed apps")
			for i := 0; i < len(appList); i++ {
				preRuleStatus, ruleName, err := Inst().Backup.CreateRuleForBackup(appList[i], orgID, "pre")
				log.FailOnError(err, "Creating pre rule for deployed apps failed")
				dash.VerifyFatal(preRuleStatus, true, "Verifying pre rule for backup")
				preRuleNameList = append(preRuleNameList, ruleName)
			}
			log.InfoD("Creating post rule for deployed apps")
			for i := 0; i < len(appList); i++ {
				postRuleStatus, ruleName, err := Inst().Backup.CreateRuleForBackup(appList[i], orgID, "post")
				log.FailOnError(err, "Creating post rule for deployed apps failed")
				dash.VerifyFatal(postRuleStatus, true, "Verifying Post rule for backup")
				postRuleNameList = append(postRuleNameList, ruleName)
			}
		})

		Step("Creating cloud credentials", func() {
			log.InfoD("Creating cloud credentials")
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			for _, provider := range providers {
				credName = fmt.Sprintf("%s-%s-%v", "cred", provider, time.Now().Unix())
				CloudCredUID = uuid.New()
				CloudCredUIDMap[CloudCredUID] = credName
				err := CreateCloudCredential(provider, credName, CloudCredUID, orgID, ctx)
				dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying creation of cloud credential named [%s] for org [%s] with [%s] as provider", credName, orgID, provider))
			}
		})

		Step("Creating backup location", func() {
			log.InfoD("Creating backup location")
			for _, provider := range providers {
				backupLocation = fmt.Sprintf("autogenerated-backup-location-%v", time.Now().Unix())
				BackupLocationUID = uuid.New()
				BackupLocationMap[BackupLocationUID] = backupLocation
				err := CreateBackupLocation(provider, backupLocation, BackupLocationUID, credName, CloudCredUID,
					getGlobalBucketName(provider), orgID, "")
				dash.VerifyFatal(err, nil, "Creating backup location")
				log.InfoD("Created Backup Location with name - %s", backupLocation)
			}
		})

		Step("Register cluster for backup", func() {
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			err = CreateSourceAndDestClusters(orgID, "", "", ctx)
			dash.VerifyFatal(err, nil, "Creating source and destination cluster")
			clusterStatus, err = Inst().Backup.GetClusterStatus(orgID, SourceClusterName, ctx)
			log.FailOnError(err, fmt.Sprintf("Fetching [%s] cluster status", SourceClusterName))
			dash.VerifyFatal(clusterStatus, api.ClusterInfo_StatusInfo_Online, fmt.Sprintf("Verifying if [%s] cluster is online", SourceClusterName))
			clusterUid, err = Inst().Backup.GetClusterUID(ctx, orgID, SourceClusterName)
			dash.VerifyFatal(err, nil, fmt.Sprintf("Fetching [%s] cluster uid", SourceClusterName))
		})

		Step("Start backup of application to bucket", func() {
			for _, namespace := range bkpNamespaces {
				ctx, err := backup.GetAdminCtxFromSecret()
				log.FailOnError(err, "Fetching px-central-admin ctx")
				preRuleUid, _ := Inst().Backup.GetRuleUid(orgID, ctx, preRuleNameList[0])
				postRuleUid, _ := Inst().Backup.GetRuleUid(orgID, ctx, postRuleNameList[0])
				backupName := fmt.Sprintf("%s-%s-%v", BackupNamePrefix, namespace, time.Now().Unix())
				backupNamespaceMap[namespace] = backupName
				err = CreateBackup(backupName, SourceClusterName, backupLocation, BackupLocationUID, []string{namespace},
					labelSelectors, orgID, clusterUid, preRuleNameList[0], preRuleUid, postRuleNameList[0], postRuleUid, ctx)
				dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying backup creation: %s", backupName))
			}
		})

		Step("Restoring the backed up application", func() {
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			for _, namespace := range bkpNamespaces {
				backupName := backupNamespaceMap[namespace]
				restoreName = fmt.Sprintf("%s-%s", "test-restore", namespace)
				err = CreateRestore(restoreName, backupName, namespaceMapping, destinationClusterName, orgID, ctx, make(map[string]string))
				dash.VerifyFatal(err, nil, "Restore failed")
			}
		})

		Step("Resize volume after the restore is completed", func() {
			log.InfoD("Resize volume after the restore is completed")
			var err error
			for _, ctx := range contexts {
				var appVolumes []*volume.Volume
				log.InfoD(fmt.Sprintf("get volumes for %s app", ctx.App.Key))
				appVolumes, err = Inst().S.GetVolumes(ctx)
				log.FailOnError(err, "Failed to get volumes for app %s", ctx.App.Key)
				dash.VerifyFatal(len(appVolumes) > 0, true, "App volumes exist?")
				var requestedVols []*volume.Volume
				log.InfoD(fmt.Sprintf("Increase volume size %s on app %s's volumes: %v",
					Inst().V.String(), ctx.App.Key, appVolumes))
				requestedVols, err = Inst().S.ResizeVolume(ctx, Inst().ConfigMap)
				log.FailOnError(err, "Volume resize successful ?")
				log.InfoD(fmt.Sprintf("validate successful volume size increase on app %s's volumes: %v",
					ctx.App.Key, appVolumes))
				for _, v := range requestedVols {
					// Need to pass token before validating volume
					params := make(map[string]string)
					if Inst().ConfigMap != "" {
						params["auth-token"], err = Inst().S.GetTokenFromConfigMap(Inst().ConfigMap)
						log.FailOnError(err, "Failed to get token from configMap")
					}
					err := Inst().V.ValidateUpdateVolume(v, params)
					dash.VerifyFatal(err, nil, fmt.Sprintf("Validate volume %v update status", v))
				}
			}
		})

		Step("Validate applications post restore", func() {
			ValidateApplications(contexts)
		})

	})

	JustAfterEach(func() {
		defer EndPxBackupTorpedoTest(contexts)
		log.InfoD("Deleting the deployed apps after the testcase")
		opts := make(map[string]bool)
		opts[SkipClusterScopedObjects] = true
		ValidateAndDestroy(contexts, opts)

		log.InfoD("Deleting backup location, cloud creds and clusters")
		ctx, err := backup.GetAdminCtxFromSecret()
		log.FailOnError(err, "Fetching px-central-admin ctx")
		CleanupCloudSettingsAndClusters(BackupLocationMap, credName, CloudCredUID, ctx)
	})
})

// Restore backup from encrypted and non-encrypted backups
var _ = Describe("{RestoreEncryptedAndNonEncryptedBackups}", func() {
	var contexts []*scheduler.Context
	var appContexts []*scheduler.Context
	backupLocationMap := make(map[string]string)
	var bkpNamespaces []string
	var backupNames []string
	var restoreNames []string
	var backupLocationNames []string
	var CloudCredUID string
	var BackupLocationUID string
	var BackupLocation1UID string
	var clusterUid string
	var clusterStatus api.ClusterInfo_StatusInfo_Status
	var CredName string
	var backupName string
	var encryptionBucketName string
	providers := getProviders()
	JustBeforeEach(func() {
		StartTorpedoTest("RestoreEncryptedAndNonEncryptedBackups", "Restore encrypted and non encrypted backups", nil, 79915)
	})
	It("Creating bucket, encrypted and non-encrypted backup location", func() {
		log.InfoD("Creating bucket, encrypted and non-encrypted backup location")
		encryptionBucketName = fmt.Sprintf("%s-%s-%v", providers[0], "encryptionbucket", time.Now().Unix())
		backupLocationName := fmt.Sprintf("%s-%s", "location", providers[0])
		backupLocationNames = append(backupLocationNames, backupLocationName)
		backupLocationName = fmt.Sprintf("%s-%s", "encryption-location", providers[0])
		backupLocationNames = append(backupLocationNames, backupLocationName)
		CredName = fmt.Sprintf("%s-%s-%v", "cred", providers[0], time.Now().Unix())
		CloudCredUID = uuid.New()
		BackupLocationUID = uuid.New()
		BackupLocation1UID = uuid.New()
		encryptionKey := "px-b@ckup-@utomat!on"
		CreateBucket(providers[0], encryptionBucketName)
		ctx, err := backup.GetAdminCtxFromSecret()
		log.FailOnError(err, "Fetching px-central-admin ctx")
		err = CreateCloudCredential(providers[0], CredName, CloudCredUID, orgID, ctx)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying creation of cloud credential named [%s] for org [%s] with [%s] as provider", CredName, orgID, providers[0]))
		err = CreateBackupLocation(providers[0], backupLocationNames[0], BackupLocationUID, CredName, CloudCredUID, getGlobalBucketName(providers[0]), orgID, "")
		dash.VerifyFatal(err, nil, fmt.Sprintf("Creating backup location %s", backupLocationNames[0]))
		backupLocationMap[BackupLocationUID] = backupLocationNames[0]
		err = CreateBackupLocation(providers[0], backupLocationNames[1], BackupLocation1UID, CredName, CloudCredUID, encryptionBucketName, orgID, encryptionKey)
		dash.VerifyFatal(err, nil, fmt.Sprintf("Creating backup location %s", backupLocationNames[1]))
		backupLocationMap[BackupLocation1UID] = backupLocationNames[1]
		log.InfoD("Deploy applications")
		contexts = make([]*scheduler.Context, 0)
		for i := 0; i < Inst().GlobalScaleFactor; i++ {
			taskName := fmt.Sprintf("%s-%d", taskNamePrefix, i)
			appContexts = ScheduleApplications(taskName)
			contexts = append(contexts, appContexts...)
			for _, ctx := range appContexts {
				ctx.ReadinessTimeout = appReadinessTimeout
				namespace := GetAppNamespace(ctx, taskName)
				bkpNamespaces = append(bkpNamespaces, namespace)
			}
		}
		Step("Register cluster for backup", func() {
			log.InfoD("Register clusters for backup")
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			err = CreateSourceAndDestClusters(orgID, "", "", ctx)
			dash.VerifyFatal(err, nil, "Creating source and destination cluster")
			clusterStatus, err = Inst().Backup.GetClusterStatus(orgID, SourceClusterName, ctx)
			log.FailOnError(err, fmt.Sprintf("Fetching [%s] cluster status", SourceClusterName))
			dash.VerifyFatal(clusterStatus, api.ClusterInfo_StatusInfo_Online, fmt.Sprintf("Verifying if [%s] cluster is online", SourceClusterName))
			clusterUid, err = Inst().Backup.GetClusterUID(ctx, orgID, SourceClusterName)
			dash.VerifyFatal(err, nil, fmt.Sprintf("Fetching [%s] cluster uid", SourceClusterName))
		})

		Step("Taking encrypted and non-encrypted backups", func() {
			log.InfoD("Taking encrypted and no-encrypted backups")
			for _, namespace := range bkpNamespaces {
				backupName = fmt.Sprintf("%s-%s-%v", BackupNamePrefix, namespace, time.Now().Unix())
				backupNames = append(backupNames, backupName)
				ctx, err := backup.GetAdminCtxFromSecret()
				log.FailOnError(err, "Fetching px-central-admin ctx")
				err = CreateBackup(backupName, SourceClusterName, backupLocationNames[0], BackupLocationUID, []string{namespace},
					nil, orgID, clusterUid, "", "", "", "", ctx)
				dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying backup creation %s", backupName))
				encryptionBackupName := fmt.Sprintf("%s-%s-%s", "encryption", BackupNamePrefix, namespace)
				backupNames = append(backupNames, encryptionBackupName)
				err = CreateBackup(encryptionBackupName, SourceClusterName, backupLocationNames[1], BackupLocation1UID, []string{namespace},
					nil, orgID, clusterUid, "", "", "", "", ctx)
				dash.VerifyFatal(err, nil, fmt.Sprintf("Verifying backup creation %s", encryptionBackupName))
			}
		})

		Step("Restoring encrypted and no-encrypted backups", func() {
			log.InfoD("Restoring encrypted and no-encrypted backups")
			restoreName := fmt.Sprintf("%s-%s-%v", restoreNamePrefix, backupNames[0], time.Now().Unix())
			restoreNames = append(restoreNames, restoreName)
			ctx, err := backup.GetAdminCtxFromSecret()
			log.FailOnError(err, "Fetching px-central-admin ctx")
			err = CreateRestore(restoreName, backupNames[0], nil, destinationClusterName, orgID, ctx, make(map[string]string))
			log.FailOnError(err, "%s restore failed", restoreName)
			time.Sleep(time.Minute * 5)
			restoreName = fmt.Sprintf("%s-%s", restoreNamePrefix, backupNames[1])
			restoreNames = append(restoreNames, restoreName)
			err = CreateRestore(restoreName, backupNames[1], nil, destinationClusterName, orgID, ctx, make(map[string]string))
			log.FailOnError(err, "%s restore failed", restoreName)
		})
	})
	JustAfterEach(func() {
		defer EndPxBackupTorpedoTest(contexts)
		log.InfoD("Deleting Restores, Backups and Backup locations, cloud account")
		ctx, err := backup.GetAdminCtxFromSecret()
		log.FailOnError(err, "Fetching px-central-admin ctx")
		for _, restore := range restoreNames {
			err = DeleteRestore(restore, orgID, ctx)
			dash.VerifyFatal(err, nil, fmt.Sprintf("Deleting Restore %s", restore))
		}
		ctx, err = backup.GetAdminCtxFromSecret()
		log.FailOnError(err, "Fetching px-central-admin ctx")
		for _, backupName := range backupNames {
			backupUID, err := Inst().Backup.GetBackupUID(ctx, backupName, orgID)
			dash.VerifyFatal(err, nil, fmt.Sprintf("Getting backup UID for backup %s", backupName))
			_, err = DeleteBackup(backupName, backupUID, orgID, ctx)
			dash.VerifyFatal(err, nil, fmt.Sprintf("Deleting backup %s", backupName))
		}
		CleanupCloudSettingsAndClusters(backupLocationMap, CredName, CloudCredUID, ctx)
		DeleteBucket(providers[0], encryptionBucketName)
	})

})
