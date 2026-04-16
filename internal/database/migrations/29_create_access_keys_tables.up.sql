--
-- Copyright (c) 2026 Red Hat Inc.
--
-- Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with
-- the License. You may obtain a copy of the License at
--
--   http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on
-- an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the
-- specific language governing permissions and limitations under the License.
--

-- Create the access_keys tables:
--
-- This migration establishes the database schema for AccessKey resources.
-- Access keys provide programmatic API access for users.
--
-- SECURITY NOTES:
-- - The secret_access_key is NEVER stored in plaintext
-- - Only the bcrypt hash (secret_hash) is stored
-- - The plaintext secret is only returned once at creation time
-- - Access key ID format: OSAC + 16 random alphanumeric characters
-- - Secret format: 40 random base64url characters
--

create table access_keys (
  id text not null primary key,
  name text not null default '',
  creation_timestamp timestamp with time zone not null default now(),
  deletion_timestamp timestamp with time zone not null default 'epoch',
  finalizers text[] not null default '{}',
  creators text[] not null default '{}',
  tenants text[] not null default '{}',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  data jsonb not null,
  version integer not null default 0,

  -- Access key specific fields
  user_id text not null,
  organization_id text not null,
  access_key_id text not null unique,  -- Public identifier (e.g., OSACAK16RANDOMCHARS)
  secret_hash text not null,            -- Bcrypt hash of the secret access key
  enabled boolean not null default true,
  last_used_time timestamp with time zone,

  -- Foreign key constraint to users table
  constraint access_keys_user_fk foreign key (user_id) references users(id) on delete cascade
);

create table archived_access_keys (
  id text not null,
  name text not null default '',
  creation_timestamp timestamp with time zone not null,
  deletion_timestamp timestamp with time zone not null,
  archival_timestamp timestamp with time zone not null default now(),
  creators text[] not null default '{}',
  tenants text[] not null default '{}',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  data jsonb not null,
  version integer not null default 0,
  user_id text not null,
  organization_id text not null,
  access_key_id text not null,
  secret_hash text not null,
  enabled boolean not null default true,
  last_used_time timestamp with time zone
);

-- Indexes for common queries and authentication lookups
create index access_keys_by_name on access_keys (name);
create index access_keys_by_user on access_keys (user_id);
create index access_keys_by_organization on access_keys (organization_id);
create index access_keys_by_owner on access_keys using gin (creators);
create index access_keys_by_tenant on access_keys using gin (tenants);
create index access_keys_by_label on access_keys using gin (labels);

-- Critical index for authentication: lookup by access_key_id
-- This must be very fast as it's used on every API request with access key auth
create unique index access_keys_by_key_id on access_keys (access_key_id) where deletion_timestamp = 'epoch';

-- Index for archived access keys
create index archived_access_keys_by_user on archived_access_keys (user_id);
create index archived_access_keys_by_organization on archived_access_keys (organization_id);
create index archived_access_keys_by_key_id on archived_access_keys (access_key_id);
