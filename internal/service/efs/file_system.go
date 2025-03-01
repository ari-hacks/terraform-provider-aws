// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package efs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/efs"
	"github.com/hashicorp/aws-sdk-go-base/v2/awsv1shim/v2/tfawserr"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/id"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	tfslices "github.com/hashicorp/terraform-provider-aws/internal/slices"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @SDKResource("aws_efs_file_system", name="File System")
// @Tags(identifierAttribute="id")
func ResourceFileSystem() *schema.Resource {
	return &schema.Resource{
		CreateWithoutTimeout: resourceFileSystemCreate,
		ReadWithoutTimeout:   resourceFileSystemRead,
		UpdateWithoutTimeout: resourceFileSystemUpdate,
		DeleteWithoutTimeout: resourceFileSystemDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		CustomizeDiff: verify.SetTagsDiff,

		Schema: map[string]*schema.Schema{
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"availability_zone_id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"availability_zone_name": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},
			"creation_token": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringLenBetween(0, 64),
			},
			"dns_name": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"encrypted": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
			"kms_key_id": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ForceNew:     true,
				ValidateFunc: verify.ValidARN,
			},
			"lifecycle_policy": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 2,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"transition_to_archive": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringInSlice(efs.TransitionToArchiveRules_Values(), false),
						},
						"transition_to_ia": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringInSlice(efs.TransitionToIARules_Values(), false),
						},
						"transition_to_primary_storage_class": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validation.StringInSlice(efs.TransitionToPrimaryStorageClassRules_Values(), false),
						},
					},
				},
			},
			"name": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"number_of_mount_targets": {
				Type:     schema.TypeInt,
				Computed: true,
			},
			"owner_id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"performance_mode": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice(efs.PerformanceMode_Values(), false),
			},
			"protection": {
				Type:     schema.TypeList,
				MaxItems: 1,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"replication_overwrite": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ValidateFunc: validation.StringInSlice([]string{
								efs.ReplicationOverwriteProtectionEnabled,
								efs.ReplicationOverwriteProtectionDisabled,
							}, false),
						},
					},
				},
			},
			"provisioned_throughput_in_mibps": {
				Type:     schema.TypeFloat,
				Optional: true,
			},
			"size_in_bytes": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"value": {
							Type:     schema.TypeInt,
							Computed: true,
						},
						"value_in_ia": {
							Type:     schema.TypeInt,
							Computed: true,
						},
						"value_in_standard": {
							Type:     schema.TypeInt,
							Computed: true,
						},
					},
				},
			},
			names.AttrTags:    tftags.TagsSchema(),
			names.AttrTagsAll: tftags.TagsSchemaComputed(),
			"throughput_mode": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      efs.ThroughputModeBursting,
				ValidateFunc: validation.StringInSlice(efs.ThroughputMode_Values(), false),
			},
		},
	}
}

func resourceFileSystemCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EFSConn(ctx)

	var creationToken string
	if v, ok := d.GetOk("creation_token"); ok {
		creationToken = v.(string)
	} else {
		creationToken = id.UniqueId()
	}
	throughputMode := d.Get("throughput_mode").(string)
	input := &efs.CreateFileSystemInput{
		CreationToken:  aws.String(creationToken),
		Tags:           getTagsIn(ctx),
		ThroughputMode: aws.String(throughputMode),
	}

	if v, ok := d.GetOk("availability_zone_name"); ok {
		input.AvailabilityZoneName = aws.String(v.(string))
	}

	if v, ok := d.GetOk("performance_mode"); ok {
		input.PerformanceMode = aws.String(v.(string))
	}

	if throughputMode == efs.ThroughputModeProvisioned {
		input.ProvisionedThroughputInMibps = aws.Float64(d.Get("provisioned_throughput_in_mibps").(float64))
	}

	encrypted, hasEncrypted := d.GetOk("encrypted")
	kmsKeyId, hasKmsKeyId := d.GetOk("kms_key_id")

	if hasEncrypted {
		input.Encrypted = aws.Bool(encrypted.(bool))
	}

	if hasKmsKeyId {
		input.KmsKeyId = aws.String(kmsKeyId.(string))
	}

	if encrypted == false && hasKmsKeyId {
		return sdkdiag.AppendFromErr(diags, errors.New("encrypted must be set to true when kms_key_id is specified"))
	}

	output, err := conn.CreateFileSystemWithContext(ctx, input)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "creating EFS file system: %s", err)
	}

	d.SetId(aws.StringValue(output.FileSystemId))

	if _, err := waitFileSystemAvailable(ctx, conn, d.Id()); err != nil {
		return sdkdiag.AppendErrorf(diags, "waiting for EFS file system (%s) create: %s", d.Id(), err)
	}

	if v, ok := d.GetOk("lifecycle_policy"); ok {
		input := &efs.PutLifecycleConfigurationInput{
			FileSystemId:      aws.String(d.Id()),
			LifecyclePolicies: expandFileSystemLifecyclePolicies(v.([]interface{})),
		}

		_, err := conn.PutLifecycleConfigurationWithContext(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "putting EFS file system (%s) lifecycle configuration: %s", d.Id(), err)
		}
	}

	if v, ok := d.GetOk("protection"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
		input := expandUpdateFileSystemProtectionInput(d.Id(), v.([]interface{})[0].(map[string]interface{}))

		_, err := conn.UpdateFileSystemProtectionWithContext(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EFS file system (%s) protection: %s", d.Id(), err)
		}
	}

	return append(diags, resourceFileSystemRead(ctx, d, meta)...)
}

func resourceFileSystemRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EFSConn(ctx)

	fs, err := FindFileSystemByID(ctx, conn, d.Id())

	if !d.IsNewResource() && tfresource.NotFound(err) {
		log.Printf("[WARN] EFS file system (%s) not found, removing from state", d.Id())
		d.SetId("")
		return diags
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading EFS file system (%s): %s", d.Id(), err)
	}

	d.Set("arn", fs.FileSystemArn)
	d.Set("availability_zone_id", fs.AvailabilityZoneId)
	d.Set("availability_zone_name", fs.AvailabilityZoneName)
	d.Set("creation_token", fs.CreationToken)
	d.Set("dns_name", meta.(*conns.AWSClient).RegionalHostname(fmt.Sprintf("%s.efs", aws.StringValue(fs.FileSystemId))))
	d.Set("encrypted", fs.Encrypted)
	d.Set("kms_key_id", fs.KmsKeyId)
	d.Set("name", fs.Name)
	d.Set("number_of_mount_targets", fs.NumberOfMountTargets)
	d.Set("owner_id", fs.OwnerId)
	d.Set("performance_mode", fs.PerformanceMode)
	if err := d.Set("protection", flattenFileSystemProtection(fs.FileSystemProtection)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting protection: %s", err)
	}
	d.Set("provisioned_throughput_in_mibps", fs.ProvisionedThroughputInMibps)
	if err := d.Set("size_in_bytes", flattenFileSystemSizeInBytes(fs.SizeInBytes)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting size_in_bytes: %s", err)
	}
	d.Set("throughput_mode", fs.ThroughputMode)

	setTagsOut(ctx, fs.Tags)

	output, err := conn.DescribeLifecycleConfigurationWithContext(ctx, &efs.DescribeLifecycleConfigurationInput{
		FileSystemId: aws.String(d.Id()),
	})

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading EFS file system (%s) lifecycle configuration: %s", d.Id(), err)
	}

	if err := d.Set("lifecycle_policy", flattenFileSystemLifecyclePolicies(output.LifecyclePolicies)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting lifecycle_policy: %s", err)
	}

	return diags
}

func resourceFileSystemUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EFSConn(ctx)

	if d.HasChanges("provisioned_throughput_in_mibps", "throughput_mode") {
		throughputMode := d.Get("throughput_mode").(string)
		input := &efs.UpdateFileSystemInput{
			FileSystemId:   aws.String(d.Id()),
			ThroughputMode: aws.String(throughputMode),
		}

		if throughputMode == efs.ThroughputModeProvisioned {
			input.ProvisionedThroughputInMibps = aws.Float64(d.Get("provisioned_throughput_in_mibps").(float64))
		}

		_, err := conn.UpdateFileSystemWithContext(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EFS file system (%s): %s", d.Id(), err)
		}

		if _, err := waitFileSystemAvailable(ctx, conn, d.Id()); err != nil {
			return sdkdiag.AppendErrorf(diags, "waiting for EFS file system (%s) update: %s", d.Id(), err)
		}
	}

	if d.HasChange("lifecycle_policy") {
		input := &efs.PutLifecycleConfigurationInput{
			FileSystemId:      aws.String(d.Id()),
			LifecyclePolicies: expandFileSystemLifecyclePolicies(d.Get("lifecycle_policy").([]interface{})),
		}

		// Prevent the following error during removal:
		// InvalidParameter: 1 validation error(s) found.
		// - missing required field, PutLifecycleConfigurationInput.LifecyclePolicies.
		if input.LifecyclePolicies == nil {
			input.LifecyclePolicies = []*efs.LifecyclePolicy{}
		}

		_, err := conn.PutLifecycleConfigurationWithContext(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "putting EFS file system (%s) lifecycle configuration: %s", d.Id(), err)
		}
	}

	if d.HasChanges("protection") {
		if v, ok := d.GetOk("protection"); ok && len(v.([]interface{})) > 0 && v.([]interface{})[0] != nil {
			input := expandUpdateFileSystemProtectionInput(d.Id(), v.([]interface{})[0].(map[string]interface{}))

			_, err := conn.UpdateFileSystemProtectionWithContext(ctx, input)

			if err != nil {
				return sdkdiag.AppendErrorf(diags, "updating EFS file system (%s) protection: %s", d.Id(), err)
			}
		}
	}

	return append(diags, resourceFileSystemRead(ctx, d, meta)...)
}

func resourceFileSystemDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EFSConn(ctx)

	log.Printf("[DEBUG] Deleting EFS file system: %s", d.Id())
	_, err := conn.DeleteFileSystemWithContext(ctx, &efs.DeleteFileSystemInput{
		FileSystemId: aws.String(d.Id()),
	})

	if tfawserr.ErrCodeEquals(err, efs.ErrCodeFileSystemNotFound) {
		return diags
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting EFS file system (%s): %s", d.Id(), err)
	}

	if _, err := waitFileSystemDeleted(ctx, conn, d.Id()); err != nil {
		return sdkdiag.AppendErrorf(diags, "waiting for EFS file system (%s) delete: %s", d.Id(), err)
	}

	return diags
}

func findFileSystem(ctx context.Context, conn *efs.EFS, input *efs.DescribeFileSystemsInput, filter tfslices.Predicate[*efs.FileSystemDescription]) (*efs.FileSystemDescription, error) {
	output, err := findFileSystems(ctx, conn, input, filter)

	if err != nil {
		return nil, err
	}

	return tfresource.AssertSinglePtrResult(output)
}

func findFileSystems(ctx context.Context, conn *efs.EFS, input *efs.DescribeFileSystemsInput, filter tfslices.Predicate[*efs.FileSystemDescription]) ([]*efs.FileSystemDescription, error) {
	var output []*efs.FileSystemDescription

	err := conn.DescribeFileSystemsPagesWithContext(ctx, input, func(page *efs.DescribeFileSystemsOutput, lastPage bool) bool {
		if page == nil {
			return !lastPage
		}

		for _, v := range page.FileSystems {
			if v != nil && filter(v) {
				output = append(output, v)
			}
		}

		return !lastPage
	})

	if tfawserr.ErrCodeEquals(err, efs.ErrCodeFileSystemNotFound) {
		return nil, &retry.NotFoundError{
			LastError:   err,
			LastRequest: input,
		}
	}

	if err != nil {
		return nil, err
	}

	return output, nil
}

func FindFileSystemByID(ctx context.Context, conn *efs.EFS, id string) (*efs.FileSystemDescription, error) {
	input := &efs.DescribeFileSystemsInput{
		FileSystemId: aws.String(id),
	}

	return findFileSystem(ctx, conn, input, tfslices.PredicateTrue[*efs.FileSystemDescription]())
}

func statusFileSystemLifeCycleState(ctx context.Context, conn *efs.EFS, id string) retry.StateRefreshFunc {
	return func() (interface{}, string, error) {
		output, err := FindFileSystemByID(ctx, conn, id)

		if tfresource.NotFound(err) {
			return nil, "", nil
		}

		if err != nil {
			return nil, "", err
		}

		return output, aws.StringValue(output.LifeCycleState), nil
	}
}

const (
	fileSystemAvailableTimeout      = 10 * time.Minute
	fileSystemAvailableDelayTimeout = 2 * time.Second
	fileSystemAvailableMinTimeout   = 3 * time.Second
	fileSystemDeletedTimeout        = 10 * time.Minute
	fileSystemDeletedDelayTimeout   = 2 * time.Second
	fileSystemDeletedMinTimeout     = 3 * time.Second
)

func waitFileSystemAvailable(ctx context.Context, conn *efs.EFS, fileSystemID string) (*efs.FileSystemDescription, error) { //nolint:unparam
	stateConf := &retry.StateChangeConf{
		Pending:    []string{efs.LifeCycleStateCreating, efs.LifeCycleStateUpdating},
		Target:     []string{efs.LifeCycleStateAvailable},
		Refresh:    statusFileSystemLifeCycleState(ctx, conn, fileSystemID),
		Timeout:    fileSystemAvailableTimeout,
		Delay:      fileSystemAvailableDelayTimeout,
		MinTimeout: fileSystemAvailableMinTimeout,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*efs.FileSystemDescription); ok {
		return output, err
	}

	return nil, err
}

func waitFileSystemDeleted(ctx context.Context, conn *efs.EFS, fileSystemID string) (*efs.FileSystemDescription, error) {
	stateConf := &retry.StateChangeConf{
		Pending:    []string{efs.LifeCycleStateAvailable, efs.LifeCycleStateDeleting},
		Target:     []string{},
		Refresh:    statusFileSystemLifeCycleState(ctx, conn, fileSystemID),
		Timeout:    fileSystemDeletedTimeout,
		Delay:      fileSystemDeletedDelayTimeout,
		MinTimeout: fileSystemDeletedMinTimeout,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*efs.FileSystemDescription); ok {
		return output, err
	}

	return nil, err
}

func flattenFileSystemLifecyclePolicies(apiObjects []*efs.LifecyclePolicy) []interface{} {
	var tfList []interface{}

	for _, apiObject := range apiObjects {
		if apiObject == nil {
			continue
		}

		tfMap := make(map[string]interface{})

		if apiObject.TransitionToArchive != nil {
			tfMap["transition_to_archive"] = aws.StringValue(apiObject.TransitionToArchive)
		}

		if apiObject.TransitionToIA != nil {
			tfMap["transition_to_ia"] = aws.StringValue(apiObject.TransitionToIA)
		}

		if apiObject.TransitionToPrimaryStorageClass != nil {
			tfMap["transition_to_primary_storage_class"] = aws.StringValue(apiObject.TransitionToPrimaryStorageClass)
		}

		tfList = append(tfList, tfMap)
	}

	return tfList
}

func expandFileSystemLifecyclePolicies(tfList []interface{}) []*efs.LifecyclePolicy {
	var apiObjects []*efs.LifecyclePolicy

	for _, tfMapRaw := range tfList {
		tfMap, ok := tfMapRaw.(map[string]interface{})

		if !ok {
			continue
		}

		apiObject := &efs.LifecyclePolicy{}

		if v, ok := tfMap["transition_to_archive"].(string); ok && v != "" {
			apiObject.TransitionToArchive = aws.String(v)
		}

		if v, ok := tfMap["transition_to_ia"].(string); ok && v != "" {
			apiObject.TransitionToIA = aws.String(v)
		}

		if v, ok := tfMap["transition_to_primary_storage_class"].(string); ok && v != "" {
			apiObject.TransitionToPrimaryStorageClass = aws.String(v)
		}

		apiObjects = append(apiObjects, apiObject)
	}

	return apiObjects
}

func flattenFileSystemSizeInBytes(sizeInBytes *efs.FileSystemSize) []interface{} {
	if sizeInBytes == nil {
		return []interface{}{}
	}

	m := map[string]interface{}{
		"value": aws.Int64Value(sizeInBytes.Value),
	}

	if sizeInBytes.ValueInIA != nil {
		m["value_in_ia"] = aws.Int64Value(sizeInBytes.ValueInIA)
	}

	if sizeInBytes.ValueInStandard != nil {
		m["value_in_standard"] = aws.Int64Value(sizeInBytes.ValueInStandard)
	}

	return []interface{}{m}
}

func expandUpdateFileSystemProtectionInput(id string, tfMap map[string]interface{}) *efs.UpdateFileSystemProtectionInput {
	if tfMap == nil {
		return nil
	}

	apiObject := &efs.UpdateFileSystemProtectionInput{
		FileSystemId: aws.String(id),
	}

	if v, ok := tfMap["replication_overwrite"].(string); ok && v != "" {
		apiObject.ReplicationOverwriteProtection = aws.String(v)
	}

	return apiObject
}

func flattenFileSystemProtection(protection *efs.FileSystemProtectionDescription) []interface{} {
	if protection == nil {
		return []interface{}{}
	}

	tfMap := map[string]interface{}{}

	if protection.ReplicationOverwriteProtection != nil {
		tfMap["replication_overwrite"] = aws.StringValue(protection.ReplicationOverwriteProtection)
	}

	return []interface{}{tfMap}
}
