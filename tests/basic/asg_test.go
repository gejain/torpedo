package tests

import (
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/portworx/sched-ops/task"
	"github.com/portworx/torpedo/drivers/node"
	"github.com/portworx/torpedo/drivers/scheduler"
	"github.com/portworx/torpedo/pkg/testrailuttils"
	. "github.com/portworx/torpedo/tests"

	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	scaleTimeout = 10 * time.Minute
)

// This test performs basic test of scaling up and down the asg cluster
var _ = Describe("{ClusterScaleUpDown}", func() {
	var testrailID = 58847
	// testrailID corresponds to: https://portworx.testrail.net/index.php?/cases/view/58847
	var runID int
	JustBeforeEach(func() {
		runID = testrailuttils.AddRunsToMilestone(testrailID)
	})
	var contexts []*scheduler.Context

	It("has to validate that storage nodes are not lost during asg scaledown", func() {
		contexts = make([]*scheduler.Context, 0)

		for i := 0; i < Inst().GlobalScaleFactor; i++ {
			contexts = append(contexts, ScheduleApplications(fmt.Sprintf("asgscaleupdown-%d", i))...)
		}

		ValidateApplications(contexts)

		intitialNodeCount, err := Inst().N.GetASGClusterSize()
		Expect(err).NotTo(HaveOccurred())

		scaleupCount := intitialNodeCount + intitialNodeCount/2
		Step(fmt.Sprintf("scale up cluster from %d to %d nodes and validate",
			intitialNodeCount, (scaleupCount/3)*3), func() {

			// After scale up, get fresh list of nodes
			// by re-initializing scheduler and volume driver
			err = Inst().S.RefreshNodeRegistry()
			Expect(err).NotTo(HaveOccurred())

			err = Inst().V.RefreshDriverEndpoints()
			Expect(err).NotTo(HaveOccurred())

			Scale(scaleupCount)
			Step(fmt.Sprintf("validate number of storage nodes after scale up"), func() {
				ValidateClusterSize(scaleupCount)
			})

		})

		Step(fmt.Sprintf("scale down cluster back to original size of %d nodes",
			intitialNodeCount), func() {
			Scale(intitialNodeCount)

			Step(fmt.Sprintf("wait for %s minutes for auto recovery of storeage nodes",
				Inst().AutoStorageNodeRecoveryTimeout.String()), func() {
				time.Sleep(Inst().AutoStorageNodeRecoveryTimeout)
			})

			// After scale down, get fresh list of nodes
			// by re-initializing scheduler and volume driver
			err = Inst().S.RefreshNodeRegistry()
			Expect(err).NotTo(HaveOccurred())

			err = Inst().V.RefreshDriverEndpoints()
			Expect(err).NotTo(HaveOccurred())

			Step(fmt.Sprintf("validate number of storage nodes after scale down"), func() {
				ValidateClusterSize(intitialNodeCount)
			})
		})

		opts := make(map[string]bool)
		opts[scheduler.OptionsWaitForResourceLeakCleanup] = true
		ValidateAndDestroy(contexts, opts)
	})
	JustAfterEach(func() {
		AfterEachTest(contexts, testrailID, runID)
	})
})

// This test randomly kills one volume driver node and ensures cluster remains
// intact by ASG
var _ = Describe("{ASGKillRandomNodes}", func() {
	var testrailID = 58848
	// testrailID corresponds to: https://portworx.testrail.net/index.php?/cases/view/58848
	var runID int
	JustBeforeEach(func() {
		runID = testrailuttils.AddRunsToMilestone(testrailID)
	})
	var contexts []*scheduler.Context

	It("keeps killing worker nodes", func() {

		var err error
		contexts = make([]*scheduler.Context, 0)

		// Get list of nodes where storage driver is installed
		storageDriverNodes := node.GetStorageDriverNodes()
		Expect(err).NotTo(HaveOccurred())

		Step("Ensure apps are deployed", func() {
			for i := 0; i < Inst().GlobalScaleFactor; i++ {
				contexts = append(contexts, ScheduleApplications(fmt.Sprintf("asgchaos-%d", i))...)
			}
		})

		ValidateApplications(contexts)

		Step("Randomly kill one storage node", func() {

			// set frequency mins depending on the chaos level
			var frequency int
			switch Inst().ChaosLevel {
			case 5:
				frequency = 15
			case 4:
				frequency = 30
			case 3:
				frequency = 45
			case 2:
				frequency = 60
			case 1:
				frequency = 90
			default:
				frequency = 30

			}
			if Inst().MinRunTimeMins == 0 {
				// Run once
				asgKillANodeAndValidate(storageDriverNodes)

				// Validate applications and tear down
				opts := make(map[string]bool)
				opts[scheduler.OptionsWaitForResourceLeakCleanup] = true
				ValidateAndDestroy(contexts, opts)
			} else {
				// Run once till timer gets triggered
				asgKillANodeAndValidate(storageDriverNodes)

				Step("validate applications", func() {
					for _, ctx := range contexts {
						ValidateContext(ctx)
					}
				})

				// Run repeatedly
				ticker := time.NewTicker(time.Duration(frequency) * time.Minute)
				stopChannel := time.After(time.Duration(Inst().MinRunTimeMins) * time.Minute)
			L:
				for {
					select {
					case <-ticker.C:
						asgKillANodeAndValidate(storageDriverNodes)

						Step("validate applications", func() {
							for _, ctx := range contexts {
								ValidateContext(ctx)
							}
						})
					case <-stopChannel:
						ticker.Stop()
						// ticker may expire/time out in between, apps may not be
						// in correct condition to be validated. Just tear them down.
						opts := make(map[string]bool)
						opts[scheduler.OptionsWaitForResourceLeakCleanup] = true
						Step("destroy apps", func() {
							for _, ctx := range contexts {
								TearDownContext(ctx, opts)
							}
						})
						break L
					}
				}
			}
		})
	})
	JustAfterEach(func() {
		AfterEachTest(contexts, testrailID, runID)
	})
})

func Scale(count int64) {
	// In multi-zone ASG cluster, node count is per zone
	zones, err := Inst().N.GetZones()
	Expect(err).NotTo(HaveOccurred())

	perZoneCount := count / int64(len(zones))

	// err = Inst().N.SetASGClusterSize(perZoneCount, scaleTimeout)
	// Expect(err).NotTo(HaveOccurred())

	t := func() (interface{}, bool, error) {

		err = Inst().N.SetASGClusterSize(perZoneCount, scaleTimeout)
		if err != nil {
			return "", true, err
		}
		return "", false, nil
	}

	_, err = task.DoRetryWithTimeout(t, 60*time.Minute, 2*time.Minute)
	Expect(err).NotTo(HaveOccurred())

}

func asgKillANodeAndValidate(storageDriverNodes []node.Node) {
	rand.Seed(time.Now().Unix())
	nodeToKill := storageDriverNodes[rand.Intn(len(storageDriverNodes))]

	Step(fmt.Sprintf("Deleting node [%v]", nodeToKill.Name), func() {
		err := Inst().N.DeleteNode(nodeToKill, nodeDeleteTimeoutMins)
		Expect(err).NotTo(HaveOccurred())
	})

	Step("Wait for 10 min. to node get replaced by autoscalling group", func() {
		time.Sleep(10 * time.Minute)
	})

	err := Inst().S.RefreshNodeRegistry()
	Expect(err).NotTo(HaveOccurred())

	err = Inst().V.RefreshDriverEndpoints()
	Expect(err).NotTo(HaveOccurred())

	Step(fmt.Sprintf("Validate number of storage nodes after killing node [%v]", nodeToKill.Name), func() {
		ValidateClusterSize(int64(len(storageDriverNodes)))
	})
}