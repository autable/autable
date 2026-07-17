// Package filestore stores user-uploaded files in S3-compatible storage.
// Each file lives under its own numeric directory: <prefix>/<id>/<name>,
// where id is the systemdb file record ID and name its stored filename.
package filestore

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Options struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
	// Prefix is the directory inside the bucket that holds all uploads.
	Prefix string
}

type S3Store struct {
	bucket string
	prefix string
	client *s3.Client
}

func NewS3Store(ctx context.Context, options Options) (*S3Store, error) {
	region := options.Region
	if region == "" {
		region = "us-east-1"
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if options.AccessKeyID != "" || options.SecretAccessKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(options.AccessKeyID, options.SecretAccessKey, ""),
		))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(clientOptions *s3.Options) {
		clientOptions.UsePathStyle = options.ForcePathStyle
		if options.Endpoint != "" {
			clientOptions.BaseEndpoint = aws.String(options.Endpoint)
		}
	})
	return &S3Store{bucket: options.Bucket, prefix: strings.Trim(options.Prefix, "/"), client: client}, nil
}

func (store *S3Store) Put(ctx context.Context, id int64, name string, contentType string, size int64, body io.Reader) error {
	_, err := store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(store.bucket),
		Key:           aws.String(store.objectKey(id, name)),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	return err
}

func (store *S3Store) Get(ctx context.Context, id int64, name string) (io.ReadCloser, error) {
	output, err := store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(store.bucket),
		Key:    aws.String(store.objectKey(id, name)),
	})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (store *S3Store) objectKey(id int64, name string) string {
	if store.prefix == "" {
		return fmt.Sprintf("%d/%s", id, name)
	}
	return fmt.Sprintf("%s/%d/%s", store.prefix, id, name)
}
