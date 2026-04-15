# Kubernetes deployment

This directory contains the manifests used to deploy the service to an Kubernetes cluster.

There are currently two variants of the manifests: one for OpenShift, intended for production environments, and another
for Kind, intended for development and testing environments.

## OpenShift

The gRPC protocol is based on HTTP2, which isn't enabled by default in OpenShift. To enable it run this command:

```shell
$ oc annotate ingresses.config/cluster ingress.operator.openshift.io/default-enable-http2=true
```

Install the _cert-manager_ operator:

```shell
$ oc new-project cert-manager-operator

$ oc create -f - <<.
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  namespace: cert-manager-operator
  name: cert-manager-operator
spec:
  upgradeStrategy: Default
.

$ oc create -f - <<.
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: openshift-operators
  name: cert-manager
spec:
  channel: stable
  installPlanApproval: Automatic
  name: cert-manager
  source: community-operators
  sourceNamespace: openshift-marketplace
.
```

Install the _trust-manager_ operator:

```shell
$ helm install trust-manager oci://quay.io/jetstack/charts/trust-manager \
--version v0.20.0 \
--namespace cert-manager-operator \
--set app.trust.namespace=cert-manager \
--set defaultPackage.enabled=false \
--wait
```

Create the default CA:

```shell
$ helm install default-ca charts/ca \
--namespace cert-manager
```

Install the _Authorino_ operator:

```shell
$ oc create -f - <<.
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: openshift-operators
  name: authorino-operator
spec:
  name: authorino-operator
  sourceNamespace: openshift-marketplace
  source: redhat-operators
  channel: stable
  installPlanApproval: Automatic
.
```

To deploy the application run this:

```shell
$ oc apply -k manifests/overlays/openshift
```

## Kind

To create the Kind cluster use a command like this:

```yaml
$ kind create cluster --config - <<.
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
name: osac
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30000
    hostPort: 8000
    listenAddress: "0.0.0.0"
.
```

The cluster uses a single port mapping: external port 8000 on the host is forwarded to internal port 30000 in the
cluster. This port is used by the Envoy Gateway for ingress traffic.

Install the _cert-manager_ operator:

```shell
$ helm install cert-manager oci://quay.io/jetstack/charts/cert-manager \
--version v1.19.1 \
--namespace cert-manager \
--create-namespace \
--set crds.enabled=true \
--wait
```

Install the _trust-manager_ operator:

```shell
$ helm install trust-manager oci://quay.io/jetstack/charts/trust-manager \
--version v0.20.0 \
--namespace cert-manager \
--set defaultPackage.enabled=false \
--wait
```

Create the default CA:

```shell
$ helm install default-ca charts/ca \
--namespace cert-manager
```

Install the _Envoy Gateway_ that provides the Gateway API implementation used for routing traffic to the services:

```shell
$ helm install envoy-gateway oci://docker.io/envoyproxy/gateway-helm \
--version v1.6.1 \
--namespace envoy-gateway \
--create-namespace \
--wait
```

Create the default gateway configuration. First, create an `EnvoyProxy` resource that configures the gateway service
to use a `NodePort` with port 30000 (the internal ingress port mapped from the host):

```shell
$ kubectl apply -f - <<.
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyProxy
metadata:
  namespace: envoy-gateway
  name: default
spec:
  provider:
    type: Kubernetes
    kubernetes:
      envoyService:
        type: NodePort
        patch:
          type: StrategicMerge
          value:
            spec:
              ports:
              - name: https
                port: 443
                nodePort: 30000
.
```

Create the default `GatewayClass` that references the `EnvoyProxy` configuration:

```shell
$ kubectl apply -f - <<.
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: default
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
  parametersRef:
    group: gateway.envoyproxy.io
    kind: EnvoyProxy
    namespace: envoy-gateway
    name: default
.
```

Create the default `Gateway` with a TLS passthrough listener:

```shell
$ kubectl apply -f - <<.
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  namespace: envoy-gateway
  name: default
spec:
  gatewayClassName: default
  listeners:
  - name: tls
    protocol: TLS
    port: 443
    tls:
      mode: Passthrough
    allowedRoutes:
      namespaces:
        from: All
.
```

Install the _Authorino_ operator:

```shell
$ kubectl apply -f https://raw.githubusercontent.com/Kuadrant/authorino-operator/refs/heads/release-v0.23.1/config/deploy/manifests.yaml
```

Deploy the application:

```shell
$ kubectl apply -k manifests/overlays/kind
```

## Manifest Structure

The manifests are organized using Kustomize with the following structure:

```
manifests/
├── base/                    # Base deployment (core services, no Keycloak)
│   ├── database/            # PostgreSQL database
│   ├── authorino/           # Authorino authorization service
│   ├── grpc-server/         # gRPC server deployment
│   ├── rest-gateway/        # REST gateway
│   ├── ingress-proxy/       # Envoy ingress proxy
│   ├── controller/          # Kubernetes controller
│   ├── client/              # Client deployment
│   └── admin/               # Admin deployment
├── components/              # Optional components
│   └── keycloak/            # Keycloak IdP (optional)
└── overlays/                # Environment-specific overlays
    ├── kind/                # Kind cluster (includes Keycloak)
    └── openshift/           # OpenShift (includes Keycloak)
```

## Optional Keycloak Component

Keycloak is deployed as an optional Kustomize component. The base deployment **does not include Keycloak**, allowing the service to run without an identity provider. This is suitable for development or testing scenarios that don't require the Organizations API.

When the Keycloak component is included, it:
- Deploys Keycloak service with PostgreSQL backend
- Configures the gRPC server with Keycloak connection parameters
- Enables the Organizations API (which requires Keycloak)

### Deploying Without Keycloak

Use the base manifests directly:

```shell
$ kubectl apply -k manifests/base
```

This deploys 4 components:
- `fulfillment-controller`
- `fulfillment-grpc-server`
- `fulfillment-ingress-proxy`
- `fulfillment-rest-gateway`

### Deploying With Keycloak

The Kind and OpenShift overlays include the Keycloak component by default:

```shell
# Kind (includes Keycloak)
$ kubectl apply -k manifests/overlays/kind

# OpenShift (includes Keycloak)
$ oc apply -k manifests/overlays/openshift
```

This deploys 5 components (the 4 above plus `keycloak-service`).

### Creating a Custom Overlay

To create your own overlay with Keycloak:

```yaml
# my-overlay/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: my-namespace

components:
- ../../components/keycloak  # Include Keycloak

resources:
- ../../base
```

To create an overlay without Keycloak, omit the `components` section:

```yaml
# my-overlay/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: my-namespace

resources:
- ../../base
```

### How the Keycloak Component Works

The Keycloak component (`manifests/components/keycloak/`) uses Kustomize's component feature to optionally extend the base deployment. It includes:

1. **Resources**: References `../../base/keycloak` which contains the Keycloak deployment and PostgreSQL database
2. **ConfigMap** (`keycloak-config.yaml`): Provides Keycloak URL, realm, and admin realm configuration
3. **Patch** (`grpc-server-patch.yaml`): Adds environment variables and command-line flags to the gRPC server:
   - `KEYCLOAK_URL`, `KEYCLOAK_REALM`, `KEYCLOAK_ADMIN_REALM`
   - `KEYCLOAK_USERNAME`, `KEYCLOAK_PASSWORD` (from Secret)
   - `--keycloak-url`, `--keycloak-username`, `--keycloak-password`, `--keycloak-realm`
   - `--grpc-authn-trusted-token-issuers`

When the component is not included, the gRPC server starts without these parameters and the Organizations API is not registered.

### Keycloak Configuration

Keycloak configuration is managed via:

- **ConfigMap** `keycloak-config`:
  - `url`: Keycloak service URL (default: `https://keycloak:443`)
  - `realm`: Application realm (default: `osac`)
  - `admin-realm`: Admin realm (default: `master`)

- **Secret** `keycloak-admin`:
  - `username`: Admin username (default: `admin`)
  - `password`: Admin password (default: `admin`)

To customize these values, create patches in your overlay:

```yaml
# my-overlay/keycloak-secret-patch.yaml
apiVersion: v1
kind: Secret
metadata:
  name: keycloak-admin
type: Opaque
stringData:
  username: myadmin
  password: mypassword
```

Then reference the patch in your kustomization:

```yaml
# my-overlay/kustomization.yaml
components:
- ../../components/keycloak

resources:
- ../../base

patches:
- path: keycloak-secret-patch.yaml
```
