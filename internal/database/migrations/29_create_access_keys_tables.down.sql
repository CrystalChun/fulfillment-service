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

-- Drop the access_keys tables

drop index if exists archived_access_keys_by_key_id;
drop index if exists archived_access_keys_by_organization;
drop index if exists archived_access_keys_by_user;
drop index if exists access_keys_by_key_id;
drop index if exists access_keys_by_label;
drop index if exists access_keys_by_tenant;
drop index if exists access_keys_by_owner;
drop index if exists access_keys_by_organization;
drop index if exists access_keys_by_user;
drop index if exists access_keys_by_name;

drop table if exists archived_access_keys;
drop table if exists access_keys;
