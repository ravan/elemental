/*
Copyright © 2025-2026 SUSE LLC
SPDX-License-Identifier: Apache-2.0

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package userdata

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/suse/elemental/v3/pkg/block"
	"github.com/suse/elemental/v3/pkg/block/lsblk"
	"github.com/suse/elemental/v3/pkg/sys"
	"github.com/suse/elemental/v3/pkg/sys/vfs"
)

const (
	filesystemMountPoint = "/run/elemental/userdata"
)

// Recognized volume labels for user data.
var volumeLabels = []string{
	"cidata",
	"CIDATA",
	"USERDATA",
	"userdata",
}

// Standard paths to look for user data within the volume.
var userDataPaths = []string{
	"user-data",
	"userdata",
	"userdata.yaml",
	"userdata.yml",
	"user-data.yaml",
	"user-data.yml",
}

// FilesystemProvider implements the Provider interface for local filesystem volumes.
type FilesystemProvider struct {
	system *sys.System
}

// NewFilesystemProvider creates a new filesystem provider.
func NewFilesystemProvider(s *sys.System) Provider {
	return &FilesystemProvider{
		system: s,
	}
}

// Name returns the provider name.
func (p *FilesystemProvider) Name() string {
	return ProviderFilesystem
}

// Available checks if a user data volume is available.
func (p *FilesystemProvider) Available(_ context.Context) bool {
	partition := p.findUserDataPartition()
	return partition != nil
}

// Fetch retrieves user data from a mounted filesystem volume.
func (p *FilesystemProvider) Fetch(_ context.Context) (*UserData, error) {
	partition := p.findUserDataPartition()
	if partition == nil {
		return nil, ErrNoUserData
	}

	// Check if already mounted
	if len(partition.MountPoints) > 0 {
		return p.readFromMountPoint(partition.MountPoints[0])
	}

	// Mount the partition temporarily
	if err := vfs.MkdirAll(p.system.FS(), filesystemMountPoint, vfs.DirPerm); err != nil {
		return nil, fmt.Errorf("creating mount point: %w", err)
	}

	if err := p.system.Mounter().Mount(partition.Path, filesystemMountPoint, partition.FileSystem, []string{"ro"}); err != nil {
		return nil, fmt.Errorf("mounting partition %s: %w", partition.Path, err)
	}

	defer func() {
		_ = p.system.Mounter().Unmount(filesystemMountPoint)
	}()

	return p.readFromMountPoint(filesystemMountPoint)
}

// findUserDataPartition looks for a partition with a recognized user data label.
func (p *FilesystemProvider) findUserDataPartition() *block.Partition {
	device := lsblk.NewLsDevice(p.system)

	parts, err := device.GetAllPartitions()
	if err != nil {
		return nil
	}

	for _, label := range volumeLabels {
		if partition := parts.GetByLabel(label); partition != nil {
			return partition
		}
	}

	return nil
}

// readFromMountPoint reads user data from the given mount point.
func (p *FilesystemProvider) readFromMountPoint(mountPoint string) (*UserData, error) {
	for _, path := range userDataPaths {
		fullPath := filepath.Join(mountPoint, path)

		data, err := p.system.FS().ReadFile(fullPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", fullPath, err)
		}

		return ParseUserData(data, p.Name())
	}

	return nil, ErrNoUserData
}
