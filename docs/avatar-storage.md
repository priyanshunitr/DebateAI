# Avatar storage setup

DebateAI uploads custom profile pictures directly from the browser to a private
Amazon S3 bucket by using a short-lived presigned PUT URL. The backend confirms
the object before storing its public CloudFront URL in MongoDB.

## Required environment variables

Add these variables to `backend/.env` in development, or to the backend runtime
environment in production:

```dotenv
AWS_REGION=ap-south-1
AWS_S3_BUCKET=debateai-avatars
AWS_S3_PUBLIC_BASE_URL=https://avatars.example.com
AWS_S3_PRESIGN_TTL_SECONDS=300
```

Do not place AWS access keys in the repository or YAML configuration. The AWS
SDK uses its default credential chain. In production, attach an IAM role to the
backend runtime. Local development can use an AWS profile or the standard
`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optional
`AWS_SESSION_TOKEN` variables.

If the S3 variables are absent, the server continues to run, DiceBear avatars
continue to work, and custom uploads return `503 Service Unavailable`.

## Bucket configuration

Keep S3 Block Public Access enabled. Serve `avatars/*` through a CloudFront
distribution whose S3 origin uses Origin Access Control (OAC). Set
`AWS_S3_PUBLIC_BASE_URL` to the CloudFront distribution URL or its custom
domain.

The bucket needs CORS permission for direct browser PUT requests. Replace the
origins with the frontend origins used by your environments:

```json
[
  {
    "AllowedOrigins": [
      "http://localhost:5173",
      "https://debateai.example.com"
    ],
    "AllowedMethods": ["PUT"],
    "AllowedHeaders": ["cache-control", "content-type", "x-amz-tagging"],
    "ExposeHeaders": ["ETag"],
    "MaxAgeSeconds": 3000
  }
]
```

The backend role needs access only to the avatar prefix:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"],
      "Resource": [
        "arn:aws:s3:::debateai-avatars/avatar-uploads/*",
        "arn:aws:s3:::debateai-avatars/avatars/*"
      ]
    }
  ]
}
```

Presigned uploads are stored under `avatar-uploads/` and tagged
`upload-state=pending`. Confirmation copies a validated upload to the separate
`avatars/` prefix and deletes the temporary object. This prevents a still-valid
presigned URL from overwriting an avatar after validation. Add this lifecycle
rule so abandoned temporary uploads are removed automatically:

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
            { "Key": "upload-state", "Value": "pending" }
          ]
        }
      },
      "Expiration": { "Days": 1 }
    }
  ]
}
```

## Upload flow

1. The frontend requests `POST /user/avatar-upload/presign` with file metadata.
2. The backend returns a five-minute S3 PUT URL for
   `avatar-uploads/{userId}/{uuid}.{extension}`.
3. The frontend uploads the file directly to S3 using the returned headers.
4. The frontend calls `POST /user/avatar-upload/confirm` with the object key.
5. The backend verifies ownership, size, declared type, and file signature.
6. The backend copies the validated object to `avatars/{userId}/...`, outside
   the scope of the upload URL, and deletes the temporary object.
7. MongoDB is updated and the user's previous managed S3 avatar is deleted.

Accepted formats are JPEG, PNG, and WebP. The maximum size is 5 MB.
