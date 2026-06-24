/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package baremetalinstance

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	bmfov1alpha1 "github.com/osac-project/bare-metal-fulfillment-operator/api/v1alpha1"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
)

func newBareMetalInstanceCR(id, namespace, name string, deletionTimestamp *metav1.Time) *bmfov1alpha1.BareMetalInstance {
	obj := &bmfov1alpha1.BareMetalInstance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				labels.BareMetalInstanceUuid: id,
			},
		},
	}
	if deletionTimestamp != nil {
		obj.SetDeletionTimestamp(deletionTimestamp)
		obj.SetFinalizers([]string{"osac.openshift.io/baremetalinstance"})
	}
	return obj
}

func hasFinalizer(bmi *privatev1.BareMetalInstance) bool {
	return slices.Contains(bmi.GetMetadata().GetFinalizers(), finalizers.Controller)
}

func newTaskForDelete(bmiID, hubID string, hubCache controllers.HubCache) *task {
	bmi := privatev1.BareMetalInstance_builder{
		Id: bmiID,
		Metadata: privatev1.Metadata_builder{
			Finalizers: []string{finalizers.Controller},
		}.Build(),
		Status: privatev1.BareMetalInstanceStatus_builder{
			Hub: hubID,
		}.Build(),
	}.Build()

	f := &function{
		logger:   logger,
		hubCache: hubCache,
	}

	return &task{
		r:                 f,
		bareMetalInstance: bmi,
	}
}

func newFakeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(bmfov1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

var _ = Describe("mutateBMI", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should set TemplateID from catalog item template reference", func() {
		catalogItemID := "catalog-item-1"
		templateID := "osac.templates.gpu_host"

		catalogItemsClient := &fakeCatalogItemsClient{
			getResponse: privatev1.BareMetalInstanceCatalogItemsGetResponse_builder{
				Object: privatev1.BareMetalInstanceCatalogItem_builder{
					Id:       catalogItemID,
					Template: templateID,
				}.Build(),
			}.Build(),
		}

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem: catalogItemID,
				}.Build(),
			}.Build(),
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(obj.Spec.TemplateID).To(Equal(templateID))
		Expect(obj.Spec.HostType).To(Equal("default"))
	})

	It("should map run_strategy ALWAYS to RunStrategy Always", func() {
		catalogItemsClient := defaultFakeCatalogItemsClient()

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem: "catalog-1",
					RunStrategy: new(privatev1.BareMetalInstanceRunStrategy_BARE_METAL_INSTANCE_RUN_STRATEGY_ALWAYS),
				}.Build(),
			}.Build(),
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(obj.Spec.RunStrategy).To(Equal(bmfov1alpha1.RunStrategyAlways))
	})

	It("should map run_strategy HALTED to RunStrategy Halted", func() {
		catalogItemsClient := defaultFakeCatalogItemsClient()

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem: "catalog-1",
					RunStrategy: new(privatev1.BareMetalInstanceRunStrategy_BARE_METAL_INSTANCE_RUN_STRATEGY_HALTED),
				}.Build(),
			}.Build(),
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(obj.Spec.RunStrategy).To(Equal(bmfov1alpha1.RunStrategyHalted))
	})

	It("should leave RunStrategy empty when run_strategy is not set", func() {
		catalogItemsClient := defaultFakeCatalogItemsClient()

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem: "catalog-1",
				}.Build(),
			}.Build(),
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(obj.Spec.RunStrategy).To(Equal(bmfov1alpha1.RunStrategyUnspecified))
	})

	It("should include sshPublicKey and userDataSecret in templateParameters", func() {
		catalogItemsClient := defaultFakeCatalogItemsClient()

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem:  "catalog-1",
					SshPublicKey: new("ssh-ed25519 AAAA... test@example.com"),
				}.Build(),
			}.Build(),
			userDataSecretName: "bmi-test-user-data",
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).ToNot(HaveOccurred())

		var params map[string]string
		Expect(json.Unmarshal([]byte(obj.Spec.TemplateParameters), &params)).To(Succeed())
		Expect(params["sshPublicKey"]).To(Equal("ssh-ed25519 AAAA... test@example.com"))
		Expect(params["userDataSecret"]).To(Equal("bmi-test-user-data"))
	})

	It("should include only sshPublicKey when no user data", func() {
		catalogItemsClient := defaultFakeCatalogItemsClient()
		sshPublicKey := "ssh-ed25519 AAAA... test@example.com"

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem:  "catalog-1",
					SshPublicKey: new(sshPublicKey),
				}.Build(),
			}.Build(),
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).ToNot(HaveOccurred())

		var params map[string]string
		Expect(json.Unmarshal([]byte(obj.Spec.TemplateParameters), &params)).To(Succeed())
		Expect(params).To(HaveKey("sshPublicKey"))
		Expect(params["sshPublicKey"]).To(Equal(sshPublicKey))
		Expect(params).ToNot(HaveKey("userDataSecret"))
	})

	It("should leave templateParameters empty when no ssh_public_key or user_data", func() {
		catalogItemsClient := defaultFakeCatalogItemsClient()

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem: "catalog-1",
				}.Build(),
			}.Build(),
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(obj.Spec.TemplateParameters).To(BeEmpty())
	})

	It("should return error when catalog item fetch fails", func() {
		catalogItemsClient := &fakeCatalogItemsClient{
			getError: errors.New("catalog item not found"),
		}

		t := &task{
			r: &function{
				logger:                              logger,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem: "missing-catalog",
				}.Build(),
			}.Build(),
		}

		var obj bmfov1alpha1.BareMetalInstance
		err := t.mutateBMI(ctx, &obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get catalog item"))
		Expect(err.Error()).To(ContainSubstring("missing-catalog"))
	})
})

var _ = Describe("update", func() {
	It("should preserve operator-managed spec fields when patching an existing CR", func() {
		ctx := context.Background()
		ctrl := gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		const (
			bmiID        = "bmi-test-id"
			hubID        = "hub-1"
			hubNamespace = "test-ns"
		)

		existingCR := &bmfov1alpha1.BareMetalInstance{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "bmi-existing",
				Labels: map[string]string{
					labels.BareMetalInstanceUuid: bmiID,
				},
			},
			Spec: bmfov1alpha1.BareMetalInstanceSpec{
				HostType:       "default",
				TemplateID:     "osac.templates.default",
				ExternalHostID: "host-42",
				HostClass:      "openstack",
				NetworkClass:   "openstack",
			},
		}

		scheme := newFakeScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingCR).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		catalogItemsClient := defaultFakeCatalogItemsClient()

		t := &task{
			r: &function{
				logger:                              logger,
				hubCache:                            hubCache,
				bareMetalInstanceCatalogItemsClient: catalogItemsClient,
			},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: bmiID,
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{finalizers.Controller},
					Tenant:     "test-tenant",
				}.Build(),
				Spec: privatev1.BareMetalInstanceSpec_builder{
					CatalogItem: "catalog-1",
				}.Build(),
				Status: privatev1.BareMetalInstanceStatus_builder{
					Hub:   hubID,
					State: privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_PROVISIONING,
				}.Build(),
			}.Build(),
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		var updatedCR bmfov1alpha1.BareMetalInstance
		err = fakeClient.Get(ctx, clnt.ObjectKey{
			Namespace: hubNamespace,
			Name:      "bmi-existing",
		}, &updatedCR)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedCR.Spec.ExternalHostID).To(Equal("host-42"),
			"ExternalHostID must be preserved — it is managed by the bare-metal-fulfillment-operator")
		Expect(updatedCR.Spec.HostClass).To(Equal("openstack"),
			"HostClass must be preserved — it is managed by the bare-metal-fulfillment-operator")
		Expect(updatedCR.Spec.NetworkClass).To(Equal("openstack"),
			"NetworkClass must be preserved — it is managed by the bare-metal-fulfillment-operator")
	})
})

var _ = Describe("delete", func() {
	const (
		bmiID        = "test-bmi-delete-id"
		hubID        = "test-hub"
		hubNamespace = "test-ns"
		crName       = "bmi-test"
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

	It("should remove finalizer when K8s object doesn't exist", func() {
		scheme := newFakeScheme()
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

		t := newTaskForDelete(bmiID, hubID, hubCache)
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeTrue())

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeFalse())
	})

	It("should call hubClient.Delete when K8s object exists without DeletionTimestamp", func() {
		cr := newBareMetalInstanceCR(bmiID, hubNamespace, crName, nil)

		scheme := newFakeScheme()

		deleteCalled := false
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cr).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, client clnt.WithWatch, obj clnt.Object, opts ...clnt.DeleteOption) error {
					deleteCalled = true
					return nil
				},
			}).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		t := newTaskForDelete(bmiID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteCalled).To(BeTrue())
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeTrue())
	})

	It("should not call hubClient.Delete when K8s object has DeletionTimestamp", func() {
		now := metav1.Now()
		cr := newBareMetalInstanceCR(bmiID, hubNamespace, crName, &now)

		scheme := newFakeScheme()

		deleteCalled := false
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cr).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, client clnt.WithWatch, obj clnt.Object, opts ...clnt.DeleteOption) error {
					deleteCalled = true
					return nil
				},
			}).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		t := newTaskForDelete(bmiID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteCalled).To(BeFalse())
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeTrue())
	})

	It("should propagate error when hub cache returns error", func() {
		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(nil, errors.New("hub not found"))

		t := newTaskForDelete(bmiID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("hub not found"))
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeTrue())
	})

	It("should remove finalizer when hub cache returns ErrHubNotFound", func() {
		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(nil, controllers.ErrHubNotFound)

		t := newTaskForDelete(bmiID, hubID, hubCache)
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeTrue())

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeFalse())
	})

	It("should remove finalizer when no hub is assigned", func() {
		bmi := privatev1.BareMetalInstance_builder{
			Id: bmiID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
			Status: privatev1.BareMetalInstanceStatus_builder{}.Build(),
		}.Build()

		f := &function{
			logger: logger,
		}

		t := &task{
			r:                 f,
			bareMetalInstance: bmi,
		}

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasFinalizer(t.bareMetalInstance)).To(BeFalse())
	})
})

var _ = Describe("ensureUserDataSecret", func() {
	const (
		bmiID        = "test-bmi-user-data"
		hubNamespace = "test-ns"
		crName       = "bmi-test"
		crUID        = "test-uid-123"
	)

	var (
		ctx   context.Context
		owner *bmfov1alpha1.BareMetalInstance
	)

	BeforeEach(func() {
		ctx = context.Background()
		owner = &bmfov1alpha1.BareMetalInstance{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      crName,
				UID:       crUID,
			},
		}
	})

	It("should create a Secret with owner reference, labels, and content", func() {
		scheme := newFakeScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		t := &task{
			r: &function{logger: logger},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: bmiID,
				Spec: privatev1.BareMetalInstanceSpec_builder{
					UserData: new("#cloud-config\npackages:\n  - vim"),
				}.Build(),
			}.Build(),
			hubNamespace:       hubNamespace,
			hubClient:          fakeClient,
			userDataSecretName: bmiID + userDataSecretSuffix,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).ToNot(HaveOccurred())

		secret := &unstructured.Unstructured{}
		secret.SetGroupVersionKind(gvks.Secret)
		err = fakeClient.Get(ctx, clnt.ObjectKey{
			Namespace: hubNamespace,
			Name:      bmiID + userDataSecretSuffix,
		}, secret)
		Expect(err).ToNot(HaveOccurred())

		stringData, found, err := unstructured.NestedMap(secret.Object, "stringData")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(stringData[userDataSecretKey]).To(Equal("#cloud-config\npackages:\n  - vim"))

		Expect(secret.GetLabels()[labels.BareMetalInstanceUuid]).To(Equal(bmiID))

		ownerRefs := secret.GetOwnerReferences()
		Expect(ownerRefs).To(HaveLen(1))
		Expect(ownerRefs[0].Name).To(Equal(crName))
		Expect(ownerRefs[0].UID).To(Equal(owner.GetUID()))
		Expect(ownerRefs[0].Kind).To(Equal("BareMetalInstance"))
	})

	It("should be idempotent when Secret already exists", func() {
		existingSecret := &unstructured.Unstructured{}
		existingSecret.SetGroupVersionKind(gvks.Secret)
		existingSecret.SetNamespace(hubNamespace)
		existingSecret.SetName(bmiID + userDataSecretSuffix)

		scheme := newFakeScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingSecret).
			Build()

		t := &task{
			r: &function{logger: logger},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: bmiID,
				Spec: privatev1.BareMetalInstanceSpec_builder{
					UserData: new("some-data"),
				}.Build(),
			}.Build(),
			hubNamespace:       hubNamespace,
			hubClient:          fakeClient,
			userDataSecretName: bmiID + userDataSecretSuffix,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should propagate error when Secret creation fails", func() {
		scheme := newFakeScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Create: func(ctx context.Context, client clnt.WithWatch, obj clnt.Object, opts ...clnt.CreateOption) error {
					return errors.New("create failed")
				},
			}).
			Build()

		t := &task{
			r: &function{logger: logger},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: bmiID,
				Spec: privatev1.BareMetalInstanceSpec_builder{
					UserData: new("some-data"),
				}.Build(),
			}.Build(),
			hubNamespace:       hubNamespace,
			hubClient:          fakeClient,
			userDataSecretName: bmiID + userDataSecretSuffix,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("create failed"))
	})

	It("should not create a Secret when userDataSecretName is empty", func() {
		scheme := newFakeScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		t := &task{
			r: &function{logger: logger},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id:   bmiID,
				Spec: privatev1.BareMetalInstanceSpec_builder{}.Build(),
			}.Build(),
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).ToNot(HaveOccurred())
	})
})

func findProtoCondition(bmi *privatev1.BareMetalInstance, condType privatev1.BareMetalInstanceConditionType) *privatev1.BareMetalInstanceCondition {
	for _, c := range bmi.GetStatus().GetConditions() {
		if c.GetType() == condType {
			return c
		}
	}
	return nil
}

var _ = Describe("syncStatus", func() {
	newTask := func(specRestartTrigger int64) *task {
		return &task{
			r: &function{logger: logger},
			bareMetalInstance: privatev1.BareMetalInstance_builder{
				Id: "bmi-sync-test",
				Spec: privatev1.BareMetalInstanceSpec_builder{
					RestartTrigger: specRestartTrigger,
				}.Build(),
				Status: privatev1.BareMetalInstanceStatus_builder{
					State: privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_PROVISIONING,
				}.Build(),
			}.Build(),
		}
	}

	It("should not change state when object is nil", func() {
		t := newTask(0)
		t.syncStatus(nil)
		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_PROVISIONING))
	})

	It("should not change state when phase is empty", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{}
		t.syncStatus(object)
		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_PROVISIONING))
	})

	It("should map Allocating phase to PROVISIONING state", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Phase: bmfov1alpha1.BareMetalInstancePhaseAllocating,
			},
		}
		t.syncStatus(object)
		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_PROVISIONING))
	})

	It("should map Progressing phase to PROVISIONING state", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Phase: bmfov1alpha1.BareMetalInstancePhaseProgressing,
			},
		}
		t.syncStatus(object)
		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_PROVISIONING))
	})

	It("should map Ready phase to RUNNING state", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Phase: bmfov1alpha1.BareMetalInstancePhaseReady,
			},
		}
		t.syncStatus(object)
		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_RUNNING))
	})

	It("should map Failed phase to FAILED state", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Phase: bmfov1alpha1.BareMetalInstancePhaseFailed,
			},
		}
		t.syncStatus(object)
		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_FAILED))
	})

	It("should map Deleting phase to DELETING state", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Phase: bmfov1alpha1.BareMetalInstancePhaseDeleting,
			},
		}
		t.syncStatus(object)
		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_DELETING))
	})

	It("should map Allocated condition to PROVISIONED with sanitized message", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionAllocated),
						Status:  metav1.ConditionTrue,
						Reason:  "HostAllocated",
						Message: "BareMetalInstance allocated a host (osac/fake-bm-host-2) from metal3",
					},
				},
			},
		}
		t.syncStatus(object)
		cond := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_PROVISIONED)
		Expect(cond).ToNot(BeNil())
		Expect(cond.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
		Expect(cond.GetReason()).To(Equal("HostAllocated"))
		Expect(cond.GetMessage()).To(Equal("BareMetalInstance successfully provisioned"))
	})

	It("should map ProvisionTemplateComplete condition to CONFIGURATION_APPLIED with sanitized message", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionProvisionTemplateComplete),
						Status:  metav1.ConditionTrue,
						Reason:  "Complete",
						Message: "Provision template completed",
					},
				},
			},
		}
		t.syncStatus(object)
		cond := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_CONFIGURATION_APPLIED)
		Expect(cond).ToNot(BeNil())
		Expect(cond.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
		Expect(cond.GetMessage()).To(Equal("Configuration successfully applied"))
	})

	It("should map Available condition to READY with sanitized message", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionAvailable),
						Status:  metav1.ConditionTrue,
						Reason:  "HostAvailable",
						Message: "Host is available",
					},
				},
			},
		}
		t.syncStatus(object)
		cond := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_READY)
		Expect(cond).ToNot(BeNil())
		Expect(cond.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
		Expect(cond.GetMessage()).To(Equal("BareMetalInstance is ready"))
	})

	It("should set RESTART_IN_PROGRESS when PowerSynced is False with Progressing reason", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionPowerSynced),
						Status:  metav1.ConditionFalse,
						Reason:  bmfov1alpha1.HostConditionReasonProgressing,
						Message: "Power cycle in progress",
					},
				},
			},
		}
		t.syncStatus(object)
		cond := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_RESTART_IN_PROGRESS)
		Expect(cond).ToNot(BeNil())
		Expect(cond.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
		Expect(cond.GetMessage()).To(Equal("Restart in progress"))
	})

	It("should set RESTART_FAILED when PowerSynced is False with IronicAPIFailure reason", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionPowerSynced),
						Status:  metav1.ConditionFalse,
						Reason:  bmfov1alpha1.HostConditionReasonIronicAPIFailure,
						Message: "Ironic API call failed",
					},
				},
			},
		}
		t.syncStatus(object)
		cond := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_RESTART_FAILED)
		Expect(cond).ToNot(BeNil())
		Expect(cond.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
		Expect(cond.GetMessage()).To(Equal("Restart failed"))
	})

	It("should clear restart conditions and sync restart trigger when PowerSynced is True", func() {
		t := newTask(42)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionPowerSynced),
						Status:  metav1.ConditionTrue,
						Reason:  bmfov1alpha1.HostConditionReasonPowerOn,
						Message: "Power on complete",
					},
				},
			},
		}
		t.syncStatus(object)

		inProgress := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_RESTART_IN_PROGRESS)
		Expect(inProgress).ToNot(BeNil())
		Expect(inProgress.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_FALSE))

		failed := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_RESTART_FAILED)
		Expect(failed).ToNot(BeNil())
		Expect(failed.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_FALSE))

		Expect(t.bareMetalInstance.GetStatus().GetRestartTrigger()).To(Equal(int64(42)))
	})

	It("should not set restart conditions when PowerSynced is False with unhandled reason", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionPowerSynced),
						Status:  metav1.ConditionFalse,
						Reason:  "UnknownReason",
						Message: "Something unexpected",
					},
				},
			},
		}
		t.syncStatus(object)

		inProgress := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_RESTART_IN_PROGRESS)
		Expect(inProgress).To(BeNil())

		failed := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_RESTART_FAILED)
		Expect(failed).To(BeNil())
	})

	It("should map multiple conditions in a single call", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Phase: bmfov1alpha1.BareMetalInstancePhaseReady,
				Conditions: []metav1.Condition{
					{
						Type:   string(bmfov1alpha1.HostConditionAllocated),
						Status: metav1.ConditionTrue,
						Reason: "HostAllocated",
					},
					{
						Type:   string(bmfov1alpha1.HostConditionProvisionTemplateComplete),
						Status: metav1.ConditionTrue,
						Reason: "Complete",
					},
					{
						Type:   string(bmfov1alpha1.HostConditionAvailable),
						Status: metav1.ConditionTrue,
						Reason: "HostAvailable",
					},
				},
			},
		}
		t.syncStatus(object)

		Expect(t.bareMetalInstance.GetStatus().GetState()).To(
			Equal(privatev1.BareMetalInstanceState_BARE_METAL_INSTANCE_STATE_RUNNING))

		provisioned := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_PROVISIONED)
		Expect(provisioned).ToNot(BeNil())
		Expect(provisioned.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))

		configApplied := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_CONFIGURATION_APPLIED)
		Expect(configApplied).ToNot(BeNil())
		Expect(configApplied.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))

		ready := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_READY)
		Expect(ready).ToNot(BeNil())
		Expect(ready.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
	})

	It("should not leak internal details from bare-metal-fulfillment-operator messages", func() {
		t := newTask(0)
		object := &bmfov1alpha1.BareMetalInstance{
			Status: bmfov1alpha1.BareMetalInstanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(bmfov1alpha1.HostConditionAllocated),
						Status:  metav1.ConditionTrue,
						Reason:  "Allocated",
						Message: "BareMetalInstance allocated a host (osac/fake-bm-host-2) from metal3",
					},
				},
			},
		}
		t.syncStatus(object)
		cond := findProtoCondition(t.bareMetalInstance,
			privatev1.BareMetalInstanceConditionType_BARE_METAL_INSTANCE_CONDITION_TYPE_PROVISIONED)
		Expect(cond).ToNot(BeNil())
		Expect(cond.GetMessage()).ToNot(ContainSubstring("osac/"))
		Expect(cond.GetMessage()).ToNot(ContainSubstring("metal3"))
		Expect(cond.GetMessage()).ToNot(ContainSubstring("fake-bm-host"))
	})
})

var _ = Describe("sanitizeConditionMessage", func() {
	It("should return tenant-facing message for Allocated condition when True", func() {
		msg := sanitizeConditionMessage(bmfov1alpha1.HostConditionAllocated, metav1.ConditionTrue)
		Expect(msg).To(Equal("BareMetalInstance successfully provisioned"))
	})

	It("should return tenant-facing message for ProvisionTemplateComplete condition when True", func() {
		msg := sanitizeConditionMessage(bmfov1alpha1.HostConditionProvisionTemplateComplete, metav1.ConditionTrue)
		Expect(msg).To(Equal("Configuration successfully applied"))
	})

	It("should return tenant-facing message for Available condition when True", func() {
		msg := sanitizeConditionMessage(bmfov1alpha1.HostConditionAvailable, metav1.ConditionTrue)
		Expect(msg).To(Equal("BareMetalInstance is ready"))
	})

	It("should return empty message for conditions with False status", func() {
		msg := sanitizeConditionMessage(bmfov1alpha1.HostConditionAllocated, metav1.ConditionFalse)
		Expect(msg).To(BeEmpty())
	})
})

var _ = Describe("mapConditionStatus", func() {
	It("should map ConditionTrue to CONDITION_STATUS_TRUE", func() {
		Expect(mapConditionStatus(metav1.ConditionTrue)).To(
			Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
	})

	It("should map ConditionFalse to CONDITION_STATUS_FALSE", func() {
		Expect(mapConditionStatus(metav1.ConditionFalse)).To(
			Equal(privatev1.ConditionStatus_CONDITION_STATUS_FALSE))
	})

	It("should map ConditionUnknown to CONDITION_STATUS_UNSPECIFIED", func() {
		Expect(mapConditionStatus(metav1.ConditionUnknown)).To(
			Equal(privatev1.ConditionStatus_CONDITION_STATUS_UNSPECIFIED))
	})
})

func defaultFakeCatalogItemsClient() *fakeCatalogItemsClient {
	return &fakeCatalogItemsClient{
		getResponse: privatev1.BareMetalInstanceCatalogItemsGetResponse_builder{
			Object: privatev1.BareMetalInstanceCatalogItem_builder{
				Template: "osac.templates.default",
			}.Build(),
		}.Build(),
	}
}

// fakeCatalogItemsClient is a simple test double for the BareMetalInstanceCatalogItemsClient.
type fakeCatalogItemsClient struct {
	privatev1.BareMetalInstanceCatalogItemsClient
	getResponse *privatev1.BareMetalInstanceCatalogItemsGetResponse
	getError    error
}

func (c *fakeCatalogItemsClient) Get(ctx context.Context, req *privatev1.BareMetalInstanceCatalogItemsGetRequest, opts ...grpc.CallOption) (*privatev1.BareMetalInstanceCatalogItemsGetResponse, error) {
	return c.getResponse, c.getError
}
