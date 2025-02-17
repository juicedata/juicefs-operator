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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	juicefsiov1 "github.com/juicedata/juicefs-cache-group-operator/api/v1"
	"github.com/juicedata/juicefs-cache-group-operator/pkg/common"
)

var _ = Describe("WarmUp Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-warmup"
		const cgName = "test-cg"
		const namespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}
		warmup := &juicefsiov1.WarmUp{}

		BeforeEach(func() {
			By("creating cache group")
			err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      cgName,
			}, &juicefsiov1.CacheGroup{})
			if err != nil && errors.IsNotFound(err) {
				cg := &juicefsiov1.CacheGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cgName,
						Namespace: namespace,
					},
					Spec: juicefsiov1.CacheGroupSpec{},
				}
				Expect(k8sClient.Create(ctx, cg)).To(Succeed())
			}

			By("creating working pod")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      common.GenWorkerName(cgName, "test"),
			}, &corev1.Pod{})
			if err != nil && errors.IsNotFound(err) {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      common.GenWorkerName(cgName, "test"),
						Namespace: namespace,
						Labels: map[string]string{
							common.LabelCacheGroup: cgName,
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "test",
						Containers: []corev1.Container{
							{
								Name:  "juicefs",
								Image: "juicedata/mount:ee-5.1.1-ca439c2",
								Command: []string{
									"sh",
									"-c",
									"/usr/bin/juicefs auth zxh-test-2\nexec /sbin/mount.juicefs zxh-test-2 /mnt/jfs -o cache-dir=/mnt/cache",
								},
							},
						},
					},
					Status: corev1.PodStatus{},
				}
				Expect(k8sClient.Create(ctx, pod)).To(Succeed())
			}

			By("creating the custom resource for the Kind WarmUp")
			err = k8sClient.Get(ctx, typeNamespacedName, warmup)
			if err != nil && errors.IsNotFound(err) {
				warmup = &juicefsiov1.WarmUp{
					TypeMeta: metav1.TypeMeta{
						Kind:       "WarmUp",
						APIVersion: "juicefs.io/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: namespace,
					},
					Spec: juicefsiov1.WarmUpSpec{
						CacheGroupName: cgName,
					},
				}
				Expect(k8sClient.Create(ctx, warmup)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &juicefsiov1.WarmUp{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance WarmUp")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			worker := &corev1.Pod{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      common.GenWorkerName(cgName, "test"),
				Namespace: namespace,
			}, worker)
			Expect(err).NotTo(HaveOccurred())
			cg := &juicefsiov1.CacheGroup{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      cgName,
				Namespace: namespace,
			}, cg)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup worker pod")
			Expect(k8sClient.Delete(ctx, worker)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &WarmUpReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the status of the resource")
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      common.GenJobName(warmup.Name),
			}, job)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
