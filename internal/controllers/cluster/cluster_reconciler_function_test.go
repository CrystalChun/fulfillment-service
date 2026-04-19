/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package cluster

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/annotations"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
)

var _ = Describe("validateTenant", func() {
	It("should succeed when exactly one tenant is assigned", func() {
		t := &task{
			cluster: privatev1.Cluster_builder{
				Id: "test-cluster",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"tenant-1"},
				}.Build(),
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail when no tenants are assigned", func() {
		t := &task{
			cluster: privatev1.Cluster_builder{
				Id: "test-cluster",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{},
				}.Build(),
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail when multiple tenants are assigned", func() {
		t := &task{
			cluster: privatev1.Cluster_builder{
				Id: "test-cluster",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"tenant-1", "tenant-2"},
				}.Build(),
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail when metadata is missing", func() {
		t := &task{
			cluster: privatev1.Cluster_builder{
				Id: "test-cluster",
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})
})

var _ = Describe("update tenant annotation", func() {
	const (
		clusterID    = "test-cluster-id"
		tenantName   = "my-tenant"
		hubID        = "test-hub"
		hubNamespace = "test-ns"
	)

	var (
		ctx  context.Context
		ctrl *gomock.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)
	})

	It("should set tenant annotation when creating a new ClusterOrder CR", func() {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{
				Group:   gvks.ClusterOrder.Group,
				Version: gvks.ClusterOrder.Version,
				Kind:    gvks.ClusterOrder.Kind + "List",
			},
			&unstructured.UnstructuredList{},
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		cluster := privatev1.Cluster_builder{
			Id: clusterID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Spec: privatev1.ClusterSpec_builder{
				Template: "test-template",
			}.Build(),
			Status: privatev1.ClusterStatus_builder{
				State: privatev1.ClusterState_CLUSTER_STATE_PROGRESSING,
				Hub:   hubID,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:         logger,
				hubCache:       hubCache,
				maskCalculator: nil,
			},
			cluster: cluster,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify the ClusterOrder CR was created with the tenant annotation
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvks.ClusterOrderList)
		err = fakeClient.List(ctx, list)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.Items).To(HaveLen(1))

		createdCR := list.Items[0]
		Expect(createdCR.GetAnnotations()).To(HaveKeyWithValue(annotations.Tenant, tenantName))
		Expect(createdCR.GetLabels()).To(HaveKeyWithValue(labels.ClusterOrderUuid, clusterID))
	})

	It("should update ClusterOrder when node set size changes on a ready cluster", func() {
		// Create an existing ClusterOrder with size 3:
		existingOrder := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": gvks.ClusterOrder.Group + "/" + gvks.ClusterOrder.Version,
				"kind":       gvks.ClusterOrder.Kind,
				"metadata": map[string]any{
					"name":      "order-abc",
					"namespace": hubNamespace,
					"labels": map[string]any{
						labels.ClusterOrderUuid: clusterID,
					},
					"annotations": map[string]any{
						annotations.Tenant: tenantName,
					},
				},
				"spec": map[string]any{
					"templateID": "test-template",
					"nodeRequests": []any{
						map[string]any{
							"resourceClass": "gpu.gb200",
							"numberOfNodes": int64(3),
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{
				Group:   gvks.ClusterOrder.Group,
				Version: gvks.ClusterOrder.Version,
				Kind:    gvks.ClusterOrder.Kind + "List",
			},
			&unstructured.UnstructuredList{},
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingOrder).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		// Create a cluster in READY state with updated node set size (5):
		cluster := privatev1.Cluster_builder{
			Id: clusterID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Spec: privatev1.ClusterSpec_builder{
				Template: "test-template",
				NodeSets: map[string]*privatev1.ClusterNodeSet{
					"gpu.gb200": privatev1.ClusterNodeSet_builder{
						HostType: "gpu.gb200",
						Size:     5,
					}.Build(),
				},
			}.Build(),
			Status: privatev1.ClusterStatus_builder{
				State: privatev1.ClusterState_CLUSTER_STATE_READY,
				Hub:   hubID,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:         logger,
				hubCache:       hubCache,
				maskCalculator: nil,
			},
			cluster: cluster,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify the ClusterOrder was patched with the new size:
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvks.ClusterOrderList)
		err = fakeClient.List(ctx, list)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.Items).To(HaveLen(1))

		updatedCR := list.Items[0]
		nodeRequests, found, err := unstructured.NestedSlice(updatedCR.Object, "spec", "nodeRequests")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nodeRequests).To(HaveLen(1))
		nodeRequest := nodeRequests[0].(map[string]any)
		Expect(nodeRequest["resourceClass"]).To(Equal("gpu.gb200"))
		Expect(nodeRequest["numberOfNodes"]).To(BeNumerically("==", 5))
	})

	It("should update ClusterOrder when node set size changes on a progressing cluster", func() {
		// Create an existing ClusterOrder with size 3:
		existingOrder := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": gvks.ClusterOrder.Group + "/" + gvks.ClusterOrder.Version,
				"kind":       gvks.ClusterOrder.Kind,
				"metadata": map[string]any{
					"name":      "order-abc",
					"namespace": hubNamespace,
					"labels": map[string]any{
						labels.ClusterOrderUuid: clusterID,
					},
					"annotations": map[string]any{
						annotations.Tenant: tenantName,
					},
				},
				"spec": map[string]any{
					"templateID": "test-template",
					"nodeRequests": []any{
						map[string]any{
							"resourceClass": "gpu.gb200",
							"numberOfNodes": int64(3),
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{
				Group:   gvks.ClusterOrder.Group,
				Version: gvks.ClusterOrder.Version,
				Kind:    gvks.ClusterOrder.Kind + "List",
			},
			&unstructured.UnstructuredList{},
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingOrder).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		// Create a cluster in PROGRESSING state with updated node set size (5):
		cluster := privatev1.Cluster_builder{
			Id: clusterID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Spec: privatev1.ClusterSpec_builder{
				Template: "test-template",
				NodeSets: map[string]*privatev1.ClusterNodeSet{
					"gpu.gb200": privatev1.ClusterNodeSet_builder{
						HostType: "gpu.gb200",
						Size:     5,
					}.Build(),
				},
			}.Build(),
			Status: privatev1.ClusterStatus_builder{
				State: privatev1.ClusterState_CLUSTER_STATE_PROGRESSING,
				Hub:   hubID,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:         logger,
				hubCache:       hubCache,
				maskCalculator: nil,
			},
			cluster: cluster,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify the ClusterOrder was patched with the new size:
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvks.ClusterOrderList)
		err = fakeClient.List(ctx, list)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.Items).To(HaveLen(1))

		updatedCR := list.Items[0]
		nodeRequests, found, err := unstructured.NestedSlice(updatedCR.Object, "spec", "nodeRequests")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nodeRequests).To(HaveLen(1))
		nodeRequest := nodeRequests[0].(map[string]any)
		Expect(nodeRequest["resourceClass"]).To(Equal("gpu.gb200"))
		Expect(nodeRequest["numberOfNodes"]).To(BeNumerically("==", 5))
	})

	It("should not update ClusterOrder when cluster is in failed state", func() {
		// Create an existing ClusterOrder with size 3:
		existingOrder := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": gvks.ClusterOrder.Group + "/" + gvks.ClusterOrder.Version,
				"kind":       gvks.ClusterOrder.Kind,
				"metadata": map[string]any{
					"name":      "order-abc",
					"namespace": hubNamespace,
					"labels": map[string]any{
						labels.ClusterOrderUuid: clusterID,
					},
					"annotations": map[string]any{
						annotations.Tenant: tenantName,
					},
				},
				"spec": map[string]any{
					"templateID": "test-template",
					"nodeRequests": []any{
						map[string]any{
							"resourceClass": "gpu.gb200",
							"numberOfNodes": int64(3),
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{
				Group:   gvks.ClusterOrder.Group,
				Version: gvks.ClusterOrder.Version,
				Kind:    gvks.ClusterOrder.Kind + "List",
			},
			&unstructured.UnstructuredList{},
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingOrder).
			Build()

		// No hubCache expectation — the reconciler should return before touching the hub.

		// Create a cluster in FAILED state with updated node set size (5):
		cluster := privatev1.Cluster_builder{
			Id: clusterID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Spec: privatev1.ClusterSpec_builder{
				Template: "test-template",
				NodeSets: map[string]*privatev1.ClusterNodeSet{
					"gpu.gb200": privatev1.ClusterNodeSet_builder{
						HostType: "gpu.gb200",
						Size:     5,
					}.Build(),
				},
			}.Build(),
			Status: privatev1.ClusterStatus_builder{
				State: privatev1.ClusterState_CLUSTER_STATE_FAILED,
				Hub:   hubID,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:         logger,
				hubCache:       nil,
				maskCalculator: nil,
			},
			cluster: cluster,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify the ClusterOrder was NOT patched — size should still be 3:
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvks.ClusterOrderList)
		err = fakeClient.List(ctx, list)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.Items).To(HaveLen(1))

		unchangedCR := list.Items[0]
		nodeRequests, found, err := unstructured.NestedSlice(unchangedCR.Object, "spec", "nodeRequests")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(nodeRequests).To(HaveLen(1))
		nodeRequest := nodeRequests[0].(map[string]any)
		Expect(nodeRequest["numberOfNodes"]).To(BeNumerically("==", 3))
	})

	It("should map explicit cluster fields to ClusterOrder CR spec", func() {
		scheme := runtime.NewScheme()
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{
				Group:   gvks.ClusterOrder.Group,
				Version: gvks.ClusterOrder.Version,
				Kind:    gvks.ClusterOrder.Kind + "List",
			},
			&unstructured.UnstructuredList{},
		)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		pullSecret := "my-pull-secret"
		sshKey := "ssh-ed25519 AAAA..."
		releaseImage := "quay.io/openshift-release-dev/ocp-release:4.17.0-multi"
		podCIDR := "10.128.0.0/14"
		serviceCIDR := "172.30.0.0/16"

		cluster := privatev1.Cluster_builder{
			Id: clusterID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Spec: privatev1.ClusterSpec_builder{
				Template:     "test-template",
				PullSecret:   &pullSecret,
				SshPublicKey: &sshKey,
				ReleaseImage: &releaseImage,
				Network: privatev1.ClusterNetwork_builder{
					PodCidr:     &podCIDR,
					ServiceCidr: &serviceCIDR,
				}.Build(),
			}.Build(),
			Status: privatev1.ClusterStatus_builder{
				State: privatev1.ClusterState_CLUSTER_STATE_PROGRESSING,
				Hub:   hubID,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:         logger,
				hubCache:       hubCache,
				maskCalculator: nil,
			},
			cluster: cluster,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify the ClusterOrder CR spec contains the explicit fields
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvks.ClusterOrderList)
		err = fakeClient.List(ctx, list)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.Items).To(HaveLen(1))

		spec, found, err := unstructured.NestedMap(list.Items[0].Object, "spec")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(spec["pullSecret"]).To(Equal(pullSecret))
		Expect(spec["sshPublicKey"]).To(Equal(sshKey))
		Expect(spec["releaseImage"]).To(Equal(releaseImage))

		network, ok := spec["network"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(network["podCIDR"]).To(Equal(podCIDR))
		Expect(network["serviceCIDR"]).To(Equal(serviceCIDR))
	})
})
