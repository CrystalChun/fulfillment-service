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

-- Create the project_memberships tables:
--
-- This migration establishes the database schema for ProjectMembership objects following the generic schema pattern.
-- ProjectMemberships define which users have access to a project and what their role is (viewer or manager).
--
-- The data column stores:
-- - spec: ProjectMembershipSpec (project, role, user)
-- - status: ProjectMembershipStatus (state, message)
-- as JSONB.
--
create table project_memberships (
  id text not null primary key,
  name text not null default '',
  creation_timestamp timestamp with time zone not null default now(),
  deletion_timestamp timestamp with time zone not null default 'epoch',
  finalizers text[] not null default '{}',
  creator text not null default '',
  tenant text not null default '',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  data jsonb not null,
  version integer not null default 0
);

create table archived_project_memberships (
  id text not null,
  name text not null default '',
  creation_timestamp timestamp with time zone not null,
  deletion_timestamp timestamp with time zone not null,
  archival_timestamp timestamp with time zone not null default now(),
  creator text not null default '',
  tenant text not null default '',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  data jsonb not null,
  version integer not null default 0
);

create index project_memberships_by_name on project_memberships (name);
create index project_memberships_by_creator on project_memberships (creator);
create index project_memberships_by_tenant on project_memberships (tenant);
create index project_memberships_by_label on project_memberships using gin (labels);

-- Project memberships must belong to a specific organization (tenant).
alter table project_memberships add constraint project_memberships_tenant_fk foreign key (tenant) references tenants (id);
