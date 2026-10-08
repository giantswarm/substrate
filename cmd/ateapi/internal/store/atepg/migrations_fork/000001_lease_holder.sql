-- Copyright 2026 Google LLC
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- +goose Up

-- A lease records who took it and when, so a client that finds it held learns
-- which operation it waits on, and the logs carry each hold's duration. A row
-- an older release writes gets an empty holder and the time it was seen.
ALTER TABLE leases
    ADD COLUMN holder      text        NOT NULL DEFAULT '',
    ADD COLUMN acquired_at timestamptz NOT NULL DEFAULT clock_timestamp();
