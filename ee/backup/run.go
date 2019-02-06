// +build !oss

/*
 * Copyright 2018 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Dgraph Community License (the "License"); you
 * may not use this file except in compliance with the License. You
 * may obtain a copy of the License at
 *
 *     https://github.com/dgraph-io/dgraph/blob/master/licenses/DCL.txt
 */

package backup

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger"
	"github.com/dgraph-io/badger/options"
	"github.com/dgraph-io/dgraph/x"
	"github.com/spf13/cobra"
)

var Restore x.SubCommand

var opt struct {
	location string
	pdir     string
	since    uint64
}

func init() {
	Restore.Cmd = &cobra.Command{
		Use:   "restore",
		Short: "Run Dgraph (EE) Restore backup",
		Long: `
Restore loads objects created with the backup feature in Dgraph Enterprise Edition (EE).

Backups are originated from HTTP at /admin/backup, then can be restored using CLI restore
command. Restore is intended to be used with new Dgraph clusters in offline state.

The --location flag indicates a source URI with Dgraph backup objects. This URI supports all
the schemes used for backup.

Source URI formats:
  [scheme]://[host]/[path]?[args]
  [scheme]:///[path]?[args]
  /[path]?[args] (only for local or NFS)

Source URI parts:
  scheme - service handler, one of: "s3", "minio", "file"
    host - remote address. ex: "dgraph.s3.amazonaws.com"
    path - directory, bucket or container at target. ex: "/dgraph/backups/"
    args - specific arguments that are ok to appear in logs.

The --posting flag sets the posting list parent dir to store the loaded backup files.

The --since flag will try to restore from a specific read timestamp. Each backup file has
the read timestamp in their name. If this flag is not used, the restore starts from the
latest version.

Dgraph backup creates a unique backup object for each node group, and restore will create
a posting directory 'p' matching the backup group ID. Such that a backup file
named '.../r32-g2.backup' will be loaded to posting dir 'p2'.

Usage examples:

# Restore from local dir or NFS mount:
$ dgraph restore -p . -l /var/backups/dgraph

# Restore from S3:
$ dgraph restore -p /var/db/dgraph -l s3://s3.us-west-2.amazonaws.com/srfrog/dgraph

# Restore since read timestamp 20001:
$ dgraph restore -since 20001 -p /var/db/dgraph -l s3://s3.us-west-2.amazonaws.com/srfrog/dgraph

		`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			defer x.StartProfile(Restore.Conf).Stop()
			if err := run(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}

	flag := Restore.Cmd.Flags()
	flag.StringVarP(&opt.location, "location", "l", "",
		"Sets the source location URI (required).")
	flag.StringVarP(&opt.pdir, "postings", "p", "",
		"Directory where posting lists are stored (required).")
	flag.Uint64Var(&opt.since, "since", 0,
		"Starting version for partial restore")
	_ = Restore.Cmd.MarkFlagRequired("postings")
	_ = Restore.Cmd.MarkFlagRequired("location")
}

func run() (err error) {
	fmt.Println("Restoring backups from:", opt.location)
	fmt.Println("Writing postings to:", opt.pdir)

	start := time.Now()
	defer func() {
		if err == nil {
			fmt.Printf("Restore: Time elapsed: %s\n", time.Since(start).Round(time.Second))
		}
	}()

	return runRestore(opt.pdir, opt.location, opt.since)
}

// runRestore calls badger.Load and tries to load data into a new DB.
func runRestore(pdir, location string, since uint64) error {
	// Scan location for backup files and load them. Each file represents a node group,
	// and we create a new p dir for each.
	return Load(location, since, func(r io.Reader, groupId int) error {
		fmt.Printf("--- Restoring groupId: %d, since: %d\n", groupId, since)
		bo := badger.DefaultOptions
		bo.SyncWrites = true
		bo.TableLoadingMode = options.MemoryMap
		bo.ValueThreshold = 1 << 10
		bo.NumVersionsToKeep = math.MaxInt32
		bo.Dir = filepath.Join(pdir, fmt.Sprintf("p%d", groupId))
		bo.ValueDir = bo.Dir
		db, err := badger.OpenManaged(bo)
		if err != nil {
			return err
		}
		defer db.Close()
		fmt.Println("--- Creating new db:", bo.Dir)
		return db.Load(r)
	})
}
