package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/portworx/torpedo/drivers/node"
	"github.com/portworx/torpedo/drivers/scheduler"
	"github.com/portworx/torpedo/pkg/testrailuttils"
	. "github.com/portworx/torpedo/tests"
	"github.com/sirupsen/logrus"
)

const (
	defaultWaitRebootTimeout     = 5 * time.Minute
	defaultWaitRebootRetry       = 10 * time.Second
	defaultCommandRetry          = 5 * time.Second
	defaultCommandTimeout        = 1 * time.Minute
	defaultTestConnectionTimeout = 15 * time.Minute
	defaultRebootTimeRange       = 5 * time.Minute
)

var _ = Describe("{RebootOneNode}", func() {
	var testrailID = 35266
	// testrailID corresponds to: https://portworx.testrail.net/index.php?/cases/view/35266
	var runID int
	JustBeforeEach(func() {
		runID = testrailuttils.AddRunsToMilestone(testrailID)
	})
	var contexts []*scheduler.Context

	It("has to schedule apps and reboot node(s) with volumes", func() {
		var err error
		contexts = make([]*scheduler.Context, 0)

		for i := 0; i < Inst().GlobalScaleFactor; i++ {
			contexts = append(contexts, ScheduleApplications(fmt.Sprintf("rebootonenode-%d", i))...)
		}

		ValidateApplications(contexts)

		Step("get all nodes and reboot one by one", func() {
			nodesToReboot := node.GetWorkerNodes()

			// Reboot node and check driver status
			Step(fmt.Sprintf("reboot node one at a time from the node(s): %v", nodesToReboot), func() {
				for _, n := range nodesToReboot {
					if n.IsStorageDriverInstalled {
						Step(fmt.Sprintf("reboot node: %s", n.Name), func() {
							err = Inst().N.RebootNode(n, node.RebootNodeOpts{
								Force: true,
								ConnectionOpts: node.ConnectionOpts{
									Timeout:         defaultCommandTimeout,
									TimeBeforeRetry: defaultCommandRetry,
								},
							})
							Expect(err).NotTo(HaveOccurred())
						})

						Step(fmt.Sprintf("wait for node: %s to be back up", n.Name), func() {
							err = Inst().N.TestConnection(n, node.ConnectionOpts{
								Timeout:         defaultTestConnectionTimeout,
								TimeBeforeRetry: defaultWaitRebootRetry,
							})
							Expect(err).NotTo(HaveOccurred())
						})

						Step(fmt.Sprintf("Check if node: %s rebooted in last 3 minutes", n.Name), func() {
							isNodeRebootedAndUp, err := Inst().N.IsNodeRebootedInGivenTimeRange(n, defaultRebootTimeRange)
							Expect(err).NotTo(HaveOccurred())
							if !isNodeRebootedAndUp {
								Step(fmt.Sprintf("wait for volume driver to stop on node: %v", n.Name), func() {
									err := Inst().V.WaitDriverDownOnNode(n)
									Expect(err).NotTo(HaveOccurred())
								})
							}
						})

						Step(fmt.Sprintf("wait to scheduler: %s and volume driver: %s to start",
							Inst().S.String(), Inst().V.String()), func() {

							err = Inst().S.IsNodeReady(n)
							Expect(err).NotTo(HaveOccurred())

							err = Inst().V.WaitDriverUpOnNode(n, Inst().DriverStartTimeout)
							Expect(err).NotTo(HaveOccurred())
						})

						Step("validate apps", func() {
							for _, ctx := range contexts {
								ValidateContext(ctx)
							}
						})
					}
				}
			})
		})

		Step("destroy apps", func() {
			opts := make(map[string]bool)
			opts[scheduler.OptionsWaitForResourceLeakCleanup] = true
			for _, ctx := range contexts {
				TearDownContext(ctx, opts)
			}
		})
	})
	JustAfterEach(func() {
		AfterEachTest(contexts, testrailID, runID)
	})
})

var _ = Describe("{ReallocateSharedMount}", func() {

	var testrailID = 58844
	// testrailID corresponds to: https://portworx.testrail.net/index.php?/cases/view/58844
	var runID int
	JustBeforeEach(func() {
		runID = testrailuttils.AddRunsToMilestone(testrailID)
	})
	var contexts []*scheduler.Context

	It("has to schedule apps and reboot node(s) with shared volume mounts", func() {

		//var err error
		contexts = make([]*scheduler.Context, 0)

		for i := 0; i < Inst().GlobalScaleFactor; i++ {
			contexts = append(contexts, ScheduleApplications(fmt.Sprintf("reallocate-mount-%d", i))...)
		}

		ValidateApplications(contexts)

		Step("get nodes with shared mount and reboot them", func() {
			for _, ctx := range contexts {
				vols, err := Inst().S.GetVolumes(ctx)
				Expect(err).NotTo(HaveOccurred())
				for _, vol := range vols {
					if vol.Shared {

						n, err := Inst().V.GetNodeForVolume(vol, defaultCommandTimeout, defaultCommandRetry)
						Expect(err).NotTo(HaveOccurred())
						logrus.Infof("volume %s is attached on node %s [%s]", vol.ID, n.SchedulerNodeName, n.Addresses[0])

						// Workaround to avoid PWX-24277 for now.
						Step(fmt.Sprintf("wait until volume %v status is Up", vol.ID), func() {
							prevStatus := ""
							Eventually(func() (string, error) {
								connOpts := node.ConnectionOpts{
									Timeout:         defaultCommandTimeout,
									TimeBeforeRetry: defaultCommandRetry,
									Sudo:            true,
								}
								cmd := fmt.Sprintf("pxctl volume inspect %s | grep \"Replication Status\"", vol.ID)
								volStatus, err := Inst().N.RunCommandWithNoRetry(*n, cmd, connOpts)
								if err != nil {
									logrus.Warnf("failed to get replication state of volume %v: %v", vol.ID, err)
									return "", err
								}
								if volStatus != prevStatus {
									logrus.Warnf("volume %v: %v", vol.ID, volStatus)
									prevStatus = volStatus
								}
								return volStatus, nil
							}, 30*time.Minute, 10*time.Second).Should(ContainSubstring("Up"),
								"volume %v status is not Up for app %v", vol.ID, ctx.App.Key)
						})

						err = Inst().S.DisableSchedulingOnNode(*n)
						Expect(err).NotTo(HaveOccurred())
						err = Inst().V.StopDriver([]node.Node{*n}, false, nil)
						Expect(err).NotTo(HaveOccurred())
						err = Inst().N.RebootNode(*n, node.RebootNodeOpts{
							Force: true,
							ConnectionOpts: node.ConnectionOpts{
								Timeout:         defaultCommandTimeout,
								TimeBeforeRetry: defaultCommandRetry,
							},
						})
						Expect(err).NotTo(HaveOccurred())

						// as we keep the storage driver down on node until we check if the volume, we wait a minute for
						// reboot to occur then we force driver to refresh endpoint to pick another storage node which is up
						logrus.Infof("wait for %v for node reboot", defaultCommandTimeout)
						time.Sleep(defaultCommandTimeout)

						// Start NFS server to avoid pods stuck in terminating state (PWX-24274)
						err = Inst().N.Systemctl(*n, "nfs-server.service", node.SystemctlOpts{
							Action: "start",
							ConnectionOpts: node.ConnectionOpts{
								Timeout:         5 * time.Minute,
								TimeBeforeRetry: 10 * time.Second,
							}})
						Expect(err).NotTo(HaveOccurred())

						ctx.RefreshStorageEndpoint = true
						ValidateContext(ctx)
						n2, err := Inst().V.GetNodeForVolume(vol, defaultCommandTimeout, defaultCommandRetry)
						Expect(err).NotTo(HaveOccurred())
						// the mount should move to another node otherwise fail
						Expect(n2.SchedulerNodeName).NotTo(Equal(n.SchedulerNodeName))
						logrus.Infof("volume %s is now attached on node %s [%s]", vol.ID, n2.SchedulerNodeName, n2.Addresses[0])
						StartVolDriverAndWait([]node.Node{*n})
						err = Inst().S.EnableSchedulingOnNode(*n)
						Expect(err).NotTo(HaveOccurred())
						ValidateApplications(contexts)
					}
				}
			}
		})

		Step("destroy apps", func() {
			opts := make(map[string]bool)
			opts[scheduler.OptionsWaitForResourceLeakCleanup] = true
			for _, ctx := range contexts {
				TearDownContext(ctx, opts)
			}
		})
	})
	JustAfterEach(func() {
		AfterEachTest(contexts, testrailID, runID)
	})
})