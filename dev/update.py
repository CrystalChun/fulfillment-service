# -*- coding: utf-8 -*-

#
# Copyright (c) 2026 Red Hat Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with
# the License. You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the
# specific language governing permissions and limitations under the License.
#

import hashlib
import logging

import click

from . import dirs


@click.group()
def update() -> None:
    """
    Updates generated project artifacts.
    """


@update.command(name="hashes")
def hashes() -> None:
    """
    Updates the database migrations hash.
    """
    # Compute the hash of the migration files:
    migrations_dir = dirs.project() / "internal" / "database" / "migrations"
    migration_files = migrations_dir.glob("*.up.sql")
    migration_files_sorted = sorted(migration_files)
    computed_hash_source = "".join(migration_file.name + "\n" for migration_file in migration_files_sorted)
    computed_hash_source_bytes = computed_hash_source.encode()
    computed_hash_bytes = hashlib.sha256(computed_hash_source_bytes).digest()
    computed_hash_text = computed_hash_bytes.hex()

    # Read the current hash from the file:
    hash_file = migrations_dir.parent / "migrations.sha256"
    stored_hash_text = hash_file.read_text().strip() if hash_file.exists() else ""

    # Check if the hash is already up to date:
    if stored_hash_text == computed_hash_text:
        logging.info("Database migrations hash is already up to date")
        return

    # Update the hash file:
    hash_file.write_text(computed_hash_text + "\n")
    logging.info("Database migrations hash updated to '%s'", computed_hash_text)
