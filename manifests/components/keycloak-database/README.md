# Keycloak Database Component

This Kustomize component adds Keycloak database support to the fulfillment-database PostgreSQL deployment.

## What it does

- Creates a `keycloak` database and user in the existing PostgreSQL during initialization
- Generates a client certificate with DER format for Keycloak (Java JDBC compatibility)
- Uses PostgreSQL's built-in startup script mechanism (runs on first startup or when data directory is empty)

## Usage

### Deploy fulfillment-service WITHOUT Keycloak database

Use the standard overlay:

```bash
oc apply -k manifests/overlays/openshift
```

This deploys only the `service` database and `client` user.

### Deploy fulfillment-service WITH Keycloak database

Use the overlay that includes this component:

```bash
oc apply -k manifests/overlays/openshift-with-keycloak
```

This deploys:
- `service` database with `client` user (for fulfillment-service)
- `keycloak` database with `keycloak` user (for Keycloak)
- Client certificates for both users (keycloak cert includes DER format)

## Certificate Secret

When this component is enabled, it creates:
- `fulfillment-database-keycloak-client-cert` with keys:
  - `tls.crt` - Client certificate
  - `tls.key` - Private key (PEM format)
  - `key.der` - Private key (DER format for Java/JDBC)
  - `ca.crt` - CA certificate

To use with Keycloak in a different namespace:

```bash
oc get secret fulfillment-database-keycloak-client-cert -n osac -o yaml | \
  sed 's/namespace: osac/namespace: keycloak/' | \
  oc apply -f -
```
