# Avatar storage setup

DebateAI uses one Amazon S3 bucket for custom profile pictures. The browser
uploads an image directly to S3 with a short-lived presigned PUT URL, and the
backend stores the permanent public S3 link in MongoDB.

CloudFront and a separate public base URL are not required.

## Requirements

- Use a dedicated bucket that contains only public profile pictures.
- Use a bucket name without dots so its virtual-hosted HTTPS URL works normally.
- Keep temporary `avatar-uploads/*` objects private.
- Allow public read access only to confirmed `avatars/*` objects.
- Do not grant public upload, delete, or bucket-list access.

Example bucket name:

```text
debateai-profile-images-374445650164
```

Bucket names are globally unique, so choose a different suffix if necessary.

## 1. Backend configuration

The server reads `backend/config/config.prod.yml`. Add:

```yaml
s3:
  region: "us-east-1"
  bucket: "debateai-profile-images-374445650164"
  presignTTLSeconds: 300
```

The backend automatically creates final image links in this form:

```text
https://debateai-profile-images-374445650164.s3.us-east-1.amazonaws.com/avatars/{userId}/{uuid}.png
```

No `publicBaseURL` value is needed.

These process environment variables can override the YAML settings:

```dotenv
AWS_REGION=us-east-1
AWS_S3_BUCKET=debateai-profile-images-374445650164
AWS_S3_PRESIGN_TTL_SECONDS=300
```

The repository does not automatically load `backend/.env`. Export variables in
the process that starts the Go server or configure the IDE/container to load
them.

If S3 configuration is absent, the backend continues to run, but custom avatar
uploads return `503 Service Unavailable`.

## 2. Backend AWS credentials

The AWS SDK uses its default credential chain. Prefer an IAM role attached to
the production compute service. Local development can use an AWS profile or the
standard `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optional
`AWS_SESSION_TOKEN` process variables.

Never put AWS credentials in frontend code or commit them to Git. The browser
receives only an object-specific presigned URL.

Attach this policy to the backend IAM identity after replacing the bucket name:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ManageProfileImages",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:PutObjectTagging",
        "s3:GetObject",
        "s3:DeleteObject"
      ],
      "Resource": [
        "arn:aws:s3:::debateai-profile-images-374445650164/avatar-uploads/*",
        "arn:aws:s3:::debateai-profile-images-374445650164/avatars/*"
      ]
    }
  ]
}
```

`s3:PutObjectTagging` is required because temporary objects are tagged
`upload-state=pending` and confirmed objects are tagged
`upload-state=confirmed`.

## 3. Public read access for confirmed avatars

The permanent link works only when `avatars/*` is publicly readable.

In **S3 → bucket → Permissions**, adjust Block Public Access so the bucket can
accept a public read bucket policy. Account-level or organization-level Block
Public Access must not override the bucket configuration.

Then add this bucket policy after replacing the bucket name:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "PublicReadConfirmedProfileImages",
      "Effect": "Allow",
      "Principal": "*",
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::debateai-profile-images-374445650164/avatars/*"
    }
  ]
}
```

This policy does not allow the public to upload, delete, list the bucket, or
read `avatar-uploads/*`. Because confirmed profile pictures are intentionally
public, never store private documents or sensitive images in this bucket.

## 4. Browser upload CORS

In **S3 → bucket → Permissions → Cross-origin resource sharing (CORS)**, add:

```json
[
  {
    "AllowedOrigins": [
      "http://localhost:5173",
      "https://your-frontend.example.com"
    ],
    "AllowedMethods": ["PUT"],
    "AllowedHeaders": [
      "cache-control",
      "content-type",
      "x-amz-tagging"
    ],
    "ExposeHeaders": ["ETag"],
    "MaxAgeSeconds": 3000
  }
]
```

Replace the production origin with the exact deployed frontend origin. Do not
use `"*"` for production origins.

## 5. Remove abandoned temporary uploads

The browser might upload a temporary object and close before confirmation. Add
an S3 lifecycle rule with:

- prefix: `avatar-uploads/`
- tag key: `upload-state`
- tag value: `pending`
- expiration: 1 day

Equivalent lifecycle configuration:

```json
{
  "Rules": [
    {
      "ID": "Delete abandoned avatar uploads",
      "Status": "Enabled",
      "Filter": {
        "And": {
          "Prefix": "avatar-uploads/",
          "Tags": [
            {
              "Key": "upload-state",
              "Value": "pending"
            }
          ]
        }
      },
      "Expiration": {
        "Days": 1
      }
    }
  ]
}
```

If versioning is enabled, also expire noncurrent versions so replaced or
deleted profile images do not accumulate.

## Upload behavior

1. The frontend requests a presigned upload URL.
2. The browser uploads directly to `avatar-uploads/{userId}/{uuid}.{extension}`.
3. The backend verifies ownership, size, MIME type, image signature, and S3
   entity tag.
4. The backend copies the validated object to `avatars/{userId}/...` and deletes
   the temporary object.
5. MongoDB stores the permanent S3 URL and the object key.
6. When the user changes their profile picture, the previous S3 object is
   deleted.

Accepted formats are JPEG, PNG, and WebP. The maximum size is 5 MB. Confirmed
objects use a one-year immutable browser cache because every replacement gets a
new UUID and therefore a new URL.

## Verification

1. Restart the backend after configuring S3.
2. Upload a JPEG, PNG, or WebP smaller than 5 MB from the profile page.
3. Confirm the presign endpoint returns `200`.
4. Confirm the browser's direct S3 PUT returns `200`.
5. Confirm the upload-confirmation endpoint returns `200` and an `avatarUrl`.
6. Confirm the temporary object is gone from `avatar-uploads/`.
7. Confirm the final object exists under `avatars/`.
8. Open `avatarUrl` in a private browser window; it should load without AWS
   credentials.
9. Change the profile picture and confirm the old S3 object is deleted.

## Troubleshooting

### `503 Avatar uploads are not configured`

Both region and bucket are required. Remember that `backend/.env` is not loaded
automatically.

### Backend refuses a bucket name

The direct public HTTPS implementation requires a bucket name without dots.
Create a dedicated bucket using letters, numbers, and hyphens.

### Direct S3 PUT returns `403 AccessDenied`

Check that the backend IAM identity has `s3:PutObject` and
`s3:PutObjectTagging` on `avatar-uploads/*`, and confirm the presigned URL has
not expired.

### Browser reports a CORS failure

Add the exact frontend origin and all signed upload headers to the bucket's CORS
configuration.

### Permanent image URL returns `403`

Check the public-read bucket policy for `avatars/*` and verify that bucket,
account, or organization Block Public Access settings are not overriding it.

## AWS references

- [Uploading objects with presigned URLs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/PresignedUrlUploadObject.html)
- [Required permissions for S3 operations](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-with-s3-policy-actions.html)
- [Configuring S3 CORS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/ManageCorsUsing.html)
- [S3 bucket naming rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html)
- [S3 lifecycle configuration elements](https://docs.aws.amazon.com/AmazonS3/latest/userguide/intro-lifecycle-rules.html)
