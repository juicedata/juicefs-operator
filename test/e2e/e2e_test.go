/*
Copyright 2024.

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
	"github.com/juicedata/juicefs-cache-group-operator/test/utils"
)

const (
	namespace = "juicefs-cache-group-operator-system"
	running   = "Running"
	trueValue = "true"
	image     = "registry.cn-hangzhou.aliyuncs.com/juicedata/mount:ee-5.1.2-59d9736"
)

var _ = Describe("controller", Ordered, func() {
	BeforeAll(func() {
		// By("installing prometheus operator")
		// Expect(utils.InstallPrometheusOperator()).To(Succeed())

		// By("installing the cert-manager")
		// Expect(utils.InstallCertManager()).To(Succeed())

		By("installing the minio")
		Expect(utils.InstallMinio()).To(Succeed())

		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, _ = utils.Run(cmd)

		By("creating the secret")
		Expect(utils.CreateSecret(namespace)).To(Succeed())
	})

	AfterAll(func() {
		if os.Getenv("IN_CI") == trueValue {
			return
		}
		// By("uninstalling the Prometheus manager bundle")
		// utils.UninstallPrometheusOperator()

		// By("uninstalling the cert-manager bundle")
		// utils.UninstallCertManager()

		By("uninstalling the minio")
		utils.UninstallMinio()

		By("deleting the secret")
		utils.DeleteSecret(namespace)

		By("removing manager namespace")
		cmd := exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	Context("Operator", func() {
		It("should run successfully", func() {
			var controllerPodName string
			var err error

			// projectimage stores the name of the image used in the example
			var projectimage = "example.com/juicefs-cache-group-operator:v0.0.1"

			By("building the manager(Operator) image")
			cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectimage))
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("loading the the manager(Operator) image on Kind")
			err = utils.LoadImageToKindClusterWithName(projectimage)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("installing CRDs")
			cmd = exec.Command("make", "install")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("deploying the controller-manager")
			cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectimage))
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func() error {
				// Get pod name

				cmd = exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				podNames := utils.GetNonEmptyLines(string(podOutput))
				if len(podNames) != 1 {
					return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
				}
				controllerPodName = podNames[0]
				ExpectWithOffset(2, controllerPodName).Should(ContainSubstring("controller-manager"))

				// Validate pod status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := utils.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if string(status) != running {
					return fmt.Errorf("controller pod in %s status", status)
				}
				return nil
			}
			EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("CacheGroup Controller", func() {
		cgName := "e2e-test-cachegroup"

		BeforeEach(func() {
			cmd := exec.Command("kubectl", "label", "nodes", "--all", "juicefs.io/cg-worker-", "--overwrite")
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/config/e2e-test-cachegroup.yaml", "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating cg status is up to date")
			verifyCgStatusUpToDate := func() error {
				// Validate pod status
				cmd := exec.Command("kubectl", "get",
					"cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if string(status) != "Waiting" {
					return fmt.Errorf("cg expect Waiting status, but got %s", status)
				}
				return nil
			}
			Eventually(verifyCgStatusUpToDate, time.Minute, time.Second).Should(Succeed())
		})

		AfterEach(func() {
			if os.Getenv("IN_CI") == trueValue {
				return
			}
			cmd := exec.Command("kubectl", "delete", "cachegroups.juicefs.io", cgName, "-n", namespace)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			// vetify workers is deleted
			verifyWorkerDeleted := func() error {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "--no-headers", "--ignore-not-found=true")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("check worker pods failed, %+v", err)
				}
				if len(utils.GetNonEmptyLines(string(result))) != 0 {
					return fmt.Errorf("worker pods still exists")
				}
				return nil
			}
			Eventually(verifyWorkerDeleted, time.Minute, time.Second).Should(Succeed())
		})

		RegisterFailHandler(func(message string, callerSkip ...int) {
			// print logs
			cmd := exec.Command("kubectl", "logs", "--tail=-1", "-l", "control-plane=controller-manager", "-n", namespace)
			log, _ := utils.Run(cmd)
			fmt.Println("controller-manager logs:")
			fmt.Println(string(log))

			cmd = exec.Command("kubectl", "get", "pods", "-n", namespace)
			result, _ := utils.Run(cmd)
			fmt.Println("pods:")
			fmt.Println(string(result))

			cmd = exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "--no-headers", "-o", "jsonpath='{.items[*].metadata.name}'")
			result, _ = utils.Run(cmd)
			if len(utils.GetNonEmptyLines(string(result))) == 1 && strings.TrimSpace(string(result)) != "''" {
				fmt.Println("worker pod describe:")
				cmd = exec.Command("kubectl", "describe", "pod", string(result), "-n", namespace)
				log, _ = utils.Run(cmd)
				fmt.Println(string(log))

				fmt.Println("worker pod log:")
				cmd = exec.Command("kubectl", "describe", "pod", string(result), "-n", namespace)
				log, _ = utils.Run(cmd)
				fmt.Println(string(log))
			}
			Fail(message, callerSkip...)
		})

		It("should reconcile the CacheGroup", func() {
			By("validating that the CacheGroup resource is reconciled")
			verifyCacheGroupReconciled := func() error {
				cmd := exec.Command("kubectl", "get", "cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.phase}", "-n", namespace)
				status, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())

				if string(status) != "Waiting" {
					return fmt.Errorf("CacheGroup resource in %s status", status)
				}
				return nil
			}
			Eventually(verifyCacheGroupReconciled, time.Minute, time.Second).Should(Succeed())

			// validatting workers is created
			By("validating cache group workers created")
			cmd := exec.Command("kubectl", "label", "nodes", "--all", "juicefs.io/cg-worker=true", "--overwrite")
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			verifyWorkerCreated := func() error {
				cmd = exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "--no-headers")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("worker pods not created")
				}
				if len(utils.GetNonEmptyLines(string(result))) != 3 {
					return fmt.Errorf("expect 3 worker pods created, but got %d", len(utils.GetNonEmptyLines(string(result))))
				}
				return nil
			}
			Eventually(verifyWorkerCreated, 5*time.Minute, 3*time.Second).Should(Succeed())

			By("validating that the CacheGroup worker spec")
			verifyWorkerSpec := func() error {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "-o", "json")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get worker pods failed, %+v", err)
				}
				expectCmds := "/usr/bin/juicefs auth csi-ci --token ${TOKEN} --access-key minioadmin --bucket http://test-bucket.minio.default.svc.cluster.local:9000 --secret-key ${SECRET_KEY}\nexec /sbin/mount.juicefs csi-ci /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-operator-system-e2e-test-cachegroup,cache-dir=/var/jfsCache"
				nodes := corev1.PodList{}
				err = json.Unmarshal(result, &nodes)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					checkCmd := expectCmds
					if node.Annotations[common.AnnoBackupWorker] != "" {
						checkCmd = checkCmd + ",group-backup"
					}
					ExpectWithOffset(1, node.Spec.HostNetwork).Should(BeTrue())
					ExpectWithOffset(1, node.Spec.Containers[0].Image).Should(Equal(image))
					ExpectWithOffset(1, node.Spec.Containers[0].Resources.Requests.Cpu().String()).Should(Equal("100m"))
					ExpectWithOffset(1, node.Spec.Containers[0].Resources.Requests.Memory().String()).Should(Equal("128Mi"))
					ExpectWithOffset(1, node.Spec.Containers[0].Resources.Limits.Cpu().String()).Should(Equal("1"))
					ExpectWithOffset(1, node.Spec.Containers[0].Resources.Limits.Memory().String()).Should(Equal("1Gi"))
					ExpectWithOffset(1, node.Spec.Containers[0].Command).Should(Equal([]string{"sh", "-c", checkCmd}))
					ExpectWithOffset(1, node.Spec.Volumes[0].Name).Should(Equal("jfs-cache-dir-0"))
					ExpectWithOffset(1, node.Spec.Volumes[0].HostPath.Path).Should(Equal("/var/jfsCache"))
					ExpectWithOffset(1, node.Spec.Containers[0].VolumeMounts[0].Name).Should(Equal("jfs-cache-dir-0"))
					ExpectWithOffset(1, node.Spec.Containers[0].VolumeMounts[0].MountPath).Should(Equal("/var/jfsCache"))
				}
				return nil
			}
			Eventually(verifyWorkerSpec, time.Minute, time.Second).Should(Succeed())

			By("validating cg status is up to date")
			verifyCgStatusUpToDate := func() error {
				// Validate pod status
				cmd := exec.Command("kubectl", "get",
					"cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if string(status) != "Ready" {
					return fmt.Errorf("cg expect Ready status, but got %s", status)
				}

				cmd = exec.Command("kubectl", "get",
					"cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.readyWorker}",
					"-n", namespace,
				)
				readyWorker, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if string(readyWorker) != "3" {
					return fmt.Errorf("cg expect has 3 readyWorker status, but got %s", readyWorker)
				}
				cmd = exec.Command("kubectl", "get",
					"cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.expectWorker}",
					"-n", namespace,
				)
				expectWorker, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if string(expectWorker) != "3" {
					return fmt.Errorf("cg expect has 3 expectWorker status, but got %s", expectWorker)
				}
				return nil
			}
			Eventually(verifyCgStatusUpToDate, time.Minute, time.Second).Should(Succeed())
		})

		It("should reconcile the worker with node labels update", func() {
			nodeName := utils.GetKindNodeName("worker")
			cmd := exec.Command("kubectl", "label", "nodes", nodeName, "juicefs.io/cg-worker=true", "--overwrite")
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("validating worker created")
			verifyWorkerCreated := func() error {
				expectWorkerName := common.GenWorkerName(cgName, nodeName)
				cmd = exec.Command("kubectl", "wait", "pod/"+expectWorkerName,
					"--for", "condition=Ready",
					"--namespace", namespace,
					"--timeout", "1m",
				)
				_, err = utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("wait worker pod failed, %+v", err)
				}
				return nil
			}
			Eventually(verifyWorkerCreated, time.Minute, time.Second).Should(Succeed())

			By("validating worker nodename")
			verifyWorkerNodeName := func() error {
				expectWorkerName := common.GenWorkerName(cgName, nodeName)

				// Validate pod nodeName
				cmd = exec.Command("kubectl", "get",
					"pods", expectWorkerName, "-o", "jsonpath={.spec.nodeName}",
					"-n", namespace,
				)
				expectNodeName, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				ExpectWithOffset(1, string(expectNodeName)).Should(Equal(nodeName))
				return nil
			}
			Eventually(verifyWorkerNodeName, time.Minute, 3*time.Second).Should(Succeed())

			By("validating that the clean redundant worker")
			verifyCleanRedundantWorker := func() error {
				cmd := exec.Command("kubectl", "label", "nodes", nodeName, "juicefs.io/cg-worker-", "--overwrite")
				_, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				expectWorkerName := common.GenWorkerName(cgName, nodeName)
				// Validate pod deleted
				cmd = exec.Command("kubectl", "get",
					"pods", expectWorkerName,
					"-n", namespace,
					"--ignore-not-found=true",
				)
				output, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("check worker pods failed, %+v", err)
				}
				if len(utils.GetNonEmptyLines(string(output))) != 0 {
					return fmt.Errorf("worker pod still exists, worker: %s, output: %s", expectWorkerName, string(output))
				}
				return nil
			}
			Eventually(verifyCleanRedundantWorker, 2*time.Minute, 3*time.Second).Should(Succeed())

		})

		It("should apply overwrite spec", func() {
			cmd := exec.Command("kubectl", "apply", "-f", "test/e2e/config/e2e-test-cachegroup.overwrite.yaml", "-n", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating cache group workers created")
			cmd = exec.Command("kubectl", "label", "nodes", "--all", "juicefs.io/cg-worker=true", "--overwrite")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			verifyWorkerCreated := func() error {
				cmd = exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "--no-headers")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("worker pods not created")
				}
				if len(utils.GetNonEmptyLines(string(result))) != 3 {
					return fmt.Errorf("expect 3 worker pods created, but got %d", len(utils.GetNonEmptyLines(string(result))))
				}
				return nil
			}
			Eventually(verifyWorkerCreated, 5*time.Minute, 3*time.Second).Should(Succeed())

			By("validating that the CacheGroup worker spec")
			verifyWorkerSpec := func() error {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "-o", "json")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get worker pods failed, %+v", err)
				}
				normalCmds := "/usr/bin/juicefs auth csi-ci --token ${TOKEN} --access-key minioadmin --bucket http://test-bucket.minio.default.svc.cluster.local:9000 --secret-key ${SECRET_KEY}\nexec /sbin/mount.juicefs csi-ci /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-operator-system-e2e-test-cachegroup,free-space-ratio=0.1,group-weight=200,cache-dir=/var/jfsCache"
				worker2Cmds := "/usr/bin/juicefs auth csi-ci --token ${TOKEN} --access-key minioadmin --bucket http://test-bucket.minio.default.svc.cluster.local:9000 --secret-key ${SECRET_KEY}\nexec /sbin/mount.juicefs csi-ci /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-operator-system-e2e-test-cachegroup,free-space-ratio=0.01,group-weight=100,cache-dir=/var/jfsCache-0"
				nodes := corev1.PodList{}
				err = json.Unmarshal(result, &nodes)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					if node.DeletionTimestamp != nil {
						return fmt.Errorf("worker pod is deleting")
					}
					checkCmd := normalCmds
					if node.Spec.NodeName == utils.GetKindNodeName("worker2") {
						checkCmd = worker2Cmds
					}
					if node.Annotations[common.AnnoBackupWorker] != "" {
						checkCmd = checkCmd + ",group-backup"
					}
					ExpectWithOffset(1, node.Spec.HostNetwork).Should(BeTrue())
					ExpectWithOffset(1, node.Spec.Containers[0].Image).Should(Equal(image))
					ExpectWithOffset(1, node.Spec.Containers[0].Resources.Requests.Cpu().String()).Should(Equal("100m"))
					ExpectWithOffset(1, node.Spec.Containers[0].Resources.Requests.Memory().String()).Should(Equal("128Mi"))
					ExpectWithOffset(1, node.Spec.Containers[0].Command).Should(Equal([]string{"sh", "-c", checkCmd}))
					if node.Spec.NodeName == utils.GetKindNodeName("worker2") {
						ExpectWithOffset(1, node.Spec.Containers[0].Resources.Limits.Cpu().String()).Should(Equal("2"))
						ExpectWithOffset(1, node.Spec.Containers[0].Resources.Limits.Memory().String()).Should(Equal("2Gi"))
						ExpectWithOffset(1, node.Spec.Volumes[0].Name).Should(Equal("jfs-cache-dir-0"))
						ExpectWithOffset(1, node.Spec.Volumes[0].HostPath.Path).Should(Equal("/data/juicefs"))
						ExpectWithOffset(1, node.Spec.Containers[0].VolumeMounts[0].Name).Should(Equal("jfs-cache-dir-0"))
						ExpectWithOffset(1, node.Spec.Containers[0].VolumeMounts[0].MountPath).Should(Equal("/var/jfsCache-0"))
					} else {
						ExpectWithOffset(1, node.Spec.Containers[0].Resources.Limits.Cpu().String()).Should(Equal("1"))
						ExpectWithOffset(1, node.Spec.Containers[0].Resources.Limits.Memory().String()).Should(Equal("1Gi"))
					}
				}
				return nil
			}
			Eventually(verifyWorkerSpec, 5*time.Minute, 3*time.Second).Should(Succeed())
		})

		It("should gracefully handle member change ", func() {
			cmd := exec.Command("kubectl", "apply", "-f", "test/e2e/config/e2e-test-cachegroup.member_change.yaml", "-n", namespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			controlPlane := utils.GetKindNodeName("control-plane")
			worker := utils.GetKindNodeName("worker")
			worker2 := utils.GetKindNodeName("worker2")

			cmd = exec.Command("kubectl", "label", "nodes", controlPlane, worker, "juicefs.io/cg-worker=true", "--overwrite")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("validating cg status is up to date")
			verifyCgStatusUpToDate := func() error {
				cmd = exec.Command("kubectl", "get",
					"cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.readyWorker}",
					"-n", namespace,
				)
				readyWorker, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if string(readyWorker) != "2" {
					return fmt.Errorf("cg expect has 2 readyWorker status, but got %s", readyWorker)
				}
				return nil
			}
			Eventually(verifyCgStatusUpToDate, 5*time.Minute, 3*time.Second).Should(Succeed())

			cmd = exec.Command("kubectl", "label", "nodes", worker2, "juicefs.io/cg-worker=true", "--overwrite")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			verifyCgStatusUpToDate = func() error {
				cmd = exec.Command("kubectl", "get",
					"cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.readyWorker}",
					"-n", namespace,
				)
				readyWorker, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if string(readyWorker) != "3" {
					return fmt.Errorf("cg expect has 3 readyWorker status, but got %s", readyWorker)
				}
				return nil
			}
			Eventually(verifyCgStatusUpToDate, 5*time.Minute, 3*time.Second).Should(Succeed())

			By("validating new node should be as backup node")
			verifyBackupWorkerSpec := func() error {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "-o", "json")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get worker pods failed, %+v", err)
				}
				worker2Cmds := "/usr/bin/juicefs auth csi-ci --token ${TOKEN} --access-key minioadmin --bucket http://test-bucket.minio.default.svc.cluster.local:9000 --secret-key ${SECRET_KEY}\nexec /sbin/mount.juicefs csi-ci /mnt/jfs -o foreground,no-update,cache-group=juicefs-cache-group-operator-system-e2e-test-cachegroup,cache-dir=/var/jfsCache,group-backup"
				nodes := corev1.PodList{}
				err = json.Unmarshal(result, &nodes)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					if node.Spec.NodeName == worker2 {
						ExpectWithOffset(1, node.Spec.Containers[0].Command).Should(Equal([]string{"sh", "-c", worker2Cmds}))
					}
				}
				return nil
			}
			Eventually(verifyBackupWorkerSpec, time.Minute, time.Second).Should(Succeed())

			verifyWarmupFile := func() error {
				cmd = exec.Command("kubectl", "exec", "-i", "-n",
					namespace, "-c", common.WorkerContainerName, common.GenWorkerName("e2e-test-cachegroup", worker), "--",
					"sh", "-c", "echo 1 > /mnt/jfs/cache-group-test.txt")
				_, err = utils.Run(cmd)

				// check block bytes
				cmd = exec.Command("kubectl", "exec", "-i", "-n",
					namespace, "-c", common.WorkerContainerName, common.GenWorkerName("e2e-test-cachegroup", worker), "--",
					"sh", "-c", "cat /mnt/jfs/.stats | grep blockcache.bytes")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get block bytes failed, %+v", err)
				}
				var blockBytes int
				_, err = fmt.Sscanf(strings.Trim(string(result), "\n"), "blockcache.bytes: %d", &blockBytes)
				if err != nil {
					return fmt.Errorf("failed to scan block bytes: %v", err)
				}
				if blockBytes == 0 {
					return fmt.Errorf("block bytes is ")
				}
				return nil
			}
			Eventually(verifyWarmupFile, time.Minute, time.Second).Should(Succeed())

			cmd = exec.Command("kubectl", "label", "nodes", worker, "juicefs.io/cg-worker-")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			verifyWorkerWeightToZero := func() error {
				cmd := exec.Command("kubectl", "get", "pods", common.GenWorkerName(cgName, worker), "-n", namespace, "-o", "json")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get worker pods failed, %+v", err)
				}
				workerNode := corev1.Pod{}
				err = json.Unmarshal(result, &workerNode)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if !strings.Contains(workerNode.Spec.Containers[0].Command[2], "group-weight=0") {
					return fmt.Errorf("worker weight not set to 0")
				}
				return nil
			}
			Eventually(verifyWorkerWeightToZero, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("WarmUp Controller", func() {
		wuName := "e2e-test-warmup"
		cgName := "e2e-test-cachegroup"

		BeforeEach(func() {
			cmd := exec.Command("kubectl", "label", "nodes", "--all", "juicefs.io/cg-worker-", "--overwrite")
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/config/e2e-test-cachegroup.yaml", "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// validating workers are created
			cmd = exec.Command("kubectl", "label", "nodes", "--all", "juicefs.io/cg-worker=true", "--overwrite")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			verifyWorkerCreated := func() error {
				cmd = exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "--no-headers")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("worker pods not created")
				}
				if len(utils.GetNonEmptyLines(string(result))) != 3 {
					return fmt.Errorf("expect 3 worker pods created, but got %d", len(utils.GetNonEmptyLines(string(result))))
				}
				return nil
			}
			Eventually(verifyWorkerCreated, 5*time.Minute, 3*time.Second).Should(Succeed())

			By("validating cg ready")
			verifyCgStatusUpToDate := func() error {
				// Validate cg status
				cmd := exec.Command("kubectl", "get",
					"cachegroups.juicefs.io", cgName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())
				if string(status) != "Ready" {
					return fmt.Errorf("cg expect Ready status, but got %s", status)
				}
				return nil
			}
			Eventually(verifyCgStatusUpToDate, time.Minute, time.Second).Should(Succeed())

			time.Sleep(5 * time.Second)

			cmd = exec.Command("kubectl", "apply", "-f", "test/e2e/config/e2e-test-warmup.yaml", "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if os.Getenv("IN_CI") == trueValue {
				return
			}
			cmd := exec.Command("kubectl", "delete", "warmup.juicefs.io", wuName, "-n", namespace)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			// verify jobs are deleted
			verifyJobDeleted := func() error {
				cmd := exec.Command("kubectl", "get", "job", "-l", "app.kubernetes.io/name=juicefs-warmup-job", "-n", namespace, "--no-headers", "--ignore-not-found=true")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("check warmup job failed, %+v", err)
				}
				if len(utils.GetNonEmptyLines(string(result))) != 0 {
					return fmt.Errorf("warmup job still exists")
				}
				return nil
			}
			Eventually(verifyJobDeleted, time.Minute, time.Second).Should(Succeed())

			cmd = exec.Command("kubectl", "delete", "cachegroups.juicefs.io", cgName, "-n", namespace)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			// verify workers is deleted
			verifyWorkerDeleted := func() error {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "juicefs.io/cache-group="+cgName, "-n", namespace, "--no-headers", "--ignore-not-found=true")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("check worker pods failed, %+v", err)
				}
				if len(utils.GetNonEmptyLines(string(result))) != 0 {
					return fmt.Errorf("worker pods still exists")
				}
				return nil
			}
			Eventually(verifyWorkerDeleted, time.Minute, time.Second).Should(Succeed())
		})

		RegisterFailHandler(func(message string, callerSkip ...int) {
			// print logs
			cmd := exec.Command("kubectl", "logs", "-l", "control-plane=controller-manager", "-n", namespace)
			fmt.Println("controller-manager logs:")
			log, _ := utils.Run(cmd)
			fmt.Println(string(log))

			cmd = exec.Command("kubectl", "get", "pods", "-n", namespace)
			result, _ := utils.Run(cmd)
			fmt.Println("pods:")
			fmt.Println(string(result))

			cmd = exec.Command("kubectl", "get", "po", "-l", fmt.Sprintf("job-name=%s", common.GenJobName(wuName)), "-n", namespace, "--no-headers", "-o", "jsonpath='{.items[*].metadata.name}'")
			result, _ = utils.Run(cmd)
			if len(utils.GetNonEmptyLines(string(result))) == 1 && strings.TrimSpace(string(result)) != "''" {
				fmt.Println("warmup pod describe:")
				cmd = exec.Command("kubectl", "describe", "pod", string(result), "-n", namespace)
				log, _ = utils.Run(cmd)
				fmt.Println(string(log))

				fmt.Println("warmup pod log:")
				cmd = exec.Command("kubectl", "describe", "pod", string(result), "-n", namespace)
				log, _ = utils.Run(cmd)
				fmt.Println(string(log))
			}
			Fail(message, callerSkip...)
		})

		It("should reconcile the WarmUp", func() {
			By("validating that the WarmUp resource is reconciled")
			verifyWarmUpReconciled := func() error {
				cmd := exec.Command("kubectl", "get", "warmups.juicefs.io", wuName, "-o", "jsonpath={.status.phase}", "-n", namespace)
				status, err := utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())

				if string(status) != running {
					return fmt.Errorf("WarmUp resource in %s status", status)
				}
				return nil
			}
			Eventually(verifyWarmUpReconciled, time.Minute, time.Second).Should(Succeed())

			By("validating that the WarmUp job is created")
			verifyWarmUpJobCreated := func() error {
				cmd := exec.Command("kubectl", "get", "job", "-l", "app.kubernetes.io/name=juicefs-warmup-job", "-n", namespace, "--no-headers")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get warmup job failed, %+v", err)
				}
				if len(utils.GetNonEmptyLines(string(result))) != 1 {
					return fmt.Errorf("expect 1 warmup job created, but got %d", len(utils.GetNonEmptyLines(string(result))))
				}
				return nil
			}
			Eventually(verifyWarmUpJobCreated, 5*time.Minute, time.Second).Should(Succeed())

			By("validating warmup job running")
			verifyWarmUpJobRunning := func() error {
				cmd := exec.Command("kubectl", "get", "job", common.GenJobName(wuName), "-n", namespace, "-o", "jsonpath={.status.active}")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get warmup job failed, %+v", err)
				}
				if string(result) != "1" {
					return fmt.Errorf("expect 1 warmup job running, but got %s", string(result))
				}
				return nil
			}
			Eventually(verifyWarmUpJobRunning, 1*time.Minute, time.Second).Should(Succeed())

			By("validating that the WarmUp status is up to date")
			verifyWarmUpStatus := func() error {
				cmd := exec.Command("kubectl", "get", "warmups.juicefs.io", wuName, "-o", "jsonpath={.status.phase}", "-n", namespace)
				status, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get warmup job failed, %+v", err)
				}
				if string(status) != "Running" {
					return fmt.Errorf("warmup in %s status", status)
				}
				cmd = exec.Command("kubectl", "get", "po", "-l", fmt.Sprintf("job-name=%s", common.GenJobName(wuName)), "-n", namespace, "--no-headers")
				result, err := utils.Run(cmd)
				if err != nil {
					return fmt.Errorf("get warmup job pod failed, %+v", err)
				}
				if len(utils.GetNonEmptyLines(string(result))) != 1 {
					return fmt.Errorf("expect 1 warmup job pod running, but got %s", string(result))
				}
				return nil
			}
			Eventually(verifyWarmUpStatus, 5*time.Minute, time.Second).Should(Succeed())
		})
	})
})
