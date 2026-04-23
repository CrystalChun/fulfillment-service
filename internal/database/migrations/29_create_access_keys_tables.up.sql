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
-- This migration establishes the database schema for AccessKey resources following the generic schema pattern.
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
  version integer not null default 0
);

create table archived_access_keys (
  id text not null,
  name text not null default '',
  creation_timestamp timestamp with time zone not null,
  deletion_timestamp timestamp with time zone not null,
  archival_timestamp timestamp with time zone not null default now(),
  finalizers text[] not null default '{}',
  creators text[] not null default '{}',
  tenants text[] not null default '{}',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  data jsonb not null,
  version integer not null default 0
);

create index access_keys_by_name on access_keys (name);

create index access_keys_by_owner on access_keys using gin (creators);

create index access_keys_by_tenant on access_keys using gin (tenants);

create index access_keys_by_label on access_keys using gin (labels);

-- Index for looking up access keys by user_id
create index access_keys_by_user_id on access_keys ((data->'spec'->>'user_id'));

-- Index for looking up access keys by organization_id
create index access_keys_by_organization_id on access_keys ((data->'spec'->>'organization_id'));

-- Unique index for looking up access keys by access_key_id (for authentication)
-- Must be unique to prevent collisions and ensure secure authentication
create unique index access_keys_by_access_key_id on access_keys ((data->'spec'->>'access_key_id'));
