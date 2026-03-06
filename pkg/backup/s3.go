// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const backupPrefix = "sharkfin-backup-"

// S3Config holds the configuration for connecting to an S3-compatible store.
type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
}

// Validate checks that all required fields are set.
func (c *S3Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("S3Config: Bucket is required")
	}
	if c.Region == "" {
		return fmt.Errorf("S3Config: Region is required")
	}
	if c.AccessKey == "" {
		return fmt.Errorf("S3Config: AccessKey is required")
	}
	if c.SecretKey == "" {
		return fmt.Errorf("S3Config: SecretKey is required")
	}
	return nil
}

// client creates an s3.Client using explicit static credentials.
func (c *S3Config) client(ctx context.Context) *s3.Client {
	_ = ctx // reserved for future use (e.g., STS assume-role)

	opts := func(o *s3.Options) {
		o.Region = c.Region
		o.Credentials = credentials.NewStaticCredentialsProvider(
			c.AccessKey, c.SecretKey, "",
		)
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
			o.UsePathStyle = true
		}
	}

	return s3.New(s3.Options{}, opts)
}

// Upload writes data to the given key in the configured bucket.
func (c *S3Config) Upload(ctx context.Context, key string, data []byte) error {
	cl := c.client(ctx)
	_, err := cl.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("s3 put %q: %w", key, err)
	}
	return nil
}

// Download reads the object at the given key from the configured bucket.
func (c *S3Config) Download(ctx context.Context, key string) ([]byte, error) {
	cl := c.client(ctx)
	out, err := cl.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get %q: %w", key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 read %q: %w", key, err)
	}
	return data, nil
}

// ObjectInfo describes a backup object in S3.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// List returns all objects in the bucket with the "sharkfin-backup-" prefix.
func (c *S3Config) List(ctx context.Context) ([]ObjectInfo, error) {
	cl := c.client(ctx)
	prefix := backupPrefix

	var objects []ObjectInfo
	var continuationToken *string

	for {
		out, err := cl.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.Bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}

		for _, obj := range out.Contents {
			info := ObjectInfo{
				Key:  aws.ToString(obj.Key),
				Size: objectSize(obj),
			}
			if obj.LastModified != nil {
				info.LastModified = *obj.LastModified
			}
			objects = append(objects, info)
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return objects, nil
}

// objectSize extracts the size from an S3 Object, handling the pointer type.
func objectSize(obj s3types.Object) int64 {
	if obj.Size != nil {
		return *obj.Size
	}
	return 0
}
