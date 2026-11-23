# bmd - births, marriages, deaths

CLI that downloads marriage and impediment notice records from
`https://oag.ecitizen.go.ke/notice-board/data/{marriages,impediments}`
into SQLite, and uploads the original photos to an S3-compatible bucket.
Existing `application_number` rows are ignored on re-fetch.

## Install

```sh
go build -o bmd ./cmd/bmd
```

## Use

```sh
./bmd fetch --db bmd.db --dataset all --page-size 30
./bmd stats --db bmd.db
```

With S3 photo uploads and Cloudflare cookies:

```sh
BMD_COOKIE='cf_clearance=...' S3_ENDPOINT=https://s3.example.com S3_BUCKET=bmd-photos \
  ./bmd fetch --db bmd.db --max-pages 2 --verbose
```

Legacy `OAG_*` env vars still work as fallback for `BMD_*`.

## How it works

- Pages `?page=N&page_size=30` per dataset (`marriages`, `impediments`).
- Rows stored in SQLite table `notices`, `UNIQUE(dataset, application_number)`,
  inserted with `INSERT OR IGNORE` so duplicates are skipped with photos untouched.
- New rows download both original photos (`/files/download?...`) and upload to
  `{prefix}/{dataset}/{application_number}/{groom|bride}_original{ext}`,
  recording the stored URL back in the row.
- Without `S3_ENDPOINT` + `S3_BUCKET`, rows are stored without photo upload.

See `.env.example` and `bmd fetch --help`.

## Mise tasks

```sh
mise run build
mise run fetch -- --dataset all --max-pages 2 --verbose
mise run fetch:marriages
mise run fetch:impediments
mise run stats -- --db bmd.db
```
