// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ec2

// Exports for use in tests only.
var (
	ResourceInstanceConnectEndpoint  = newResourceInstanceConnectEndpoint
	ResourceSecurityGroupEgressRule  = newResourceSecurityGroupEgressRule
	ResourceSecurityGroupIngressRule = newResourceSecurityGroupIngressRule
	ResourceEBSFastSnapshotRestore   = newResourceEBSFastSnapshotRestore

	FindEBSFastSnapshotRestoreByID = findEBSFastSnapshotRestoreByID

	UpdateTags   = updateTags
	UpdateTagsV2 = updateTagsV2

	StopInstance = stopInstance
)
