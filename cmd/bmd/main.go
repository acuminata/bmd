package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"bmd/internal/api"
	"bmd/internal/runner"
	"bmd/internal/s3store"
	"bmd/internal/store"
)

var version = "dev"

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "fetch":
		if err := runFetch(os.Args[2:]); err != nil {
			log.Fatalf("error: %v", err)
		}
	case "stats":
		if err := runStats(os.Args[2:]); err != nil {
			log.Fatalf("error: %v", err)
		}
	case "version", "-v", "--version":
		fmt.Println("bmd", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `bmd - births, marriages, deaths notice-board downloader

Usage:
  bmd fetch [flags]     download marriages + impediments into sqlite, photos to S3
  bmd stats --db FILE   show row counts per dataset
  bmd version           print version

fetch flags (each also settable via the env var shown):
  --dataset marriages|impediments|all   (BMD_DATASET, default all)
  --db FILE                             (BMD_DB, default bmd.db)
  --base-url URL                        (BMD_BASE_URL, default https://oag.ecitizen.go.ke)
  --page-size N                         (BMD_PAGE_SIZE, default 30)
  --start-page N                        (BMD_START_PAGE, default 1)
  --max-pages N                         (BMD_MAX_PAGES, default 0 = all pages)
  --delay DURATION                      (BMD_DELAY, default 500ms; polite gap between pages)
  --timeout DURATION                    (BMD_TIMEOUT, default 30s)
  --cookie COOKIES                      (BMD_COOKIE; paste browser cookies if Cloudflare 403s)
  --s3-endpoint URL                     (S3_ENDPOINT; empty disables photo upload)
  --s3-bucket NAME                      (S3_BUCKET)
  --s3-region NAME                      (S3_REGION or AWS_REGION, default us-east-1)
  --s3-access-key KEY                   (S3_ACCESS_KEY or AWS_ACCESS_KEY_ID)
  --s3-secret-key KEY                   (S3_SECRET_KEY or AWS_SECRET_ACCESS_KEY)
  --s3-prefix PREFIX                    (S3_PREFIX, default notices)
  --s3-path-style BOOL                  (S3_PATH_STYLE, default true)
  --s3-public-url URL                   (S3_PUBLIC_URL; stored URL prefix, else s3://bucket/key)
  --photo-concurrency N                 (default 4)
  --verbose                             (BMD_VERBOSE)

Photos: originals only, stored as {prefix}/{dataset}/{application_number}/{groom|bride}_original{ext}.
Existing application_number rows are skipped (INSERT OR IGNORE) including their photos.

Examples:
  bmd fetch --db bmd.db --dataset all
  BMD_COOKIE='cf_clearance=...' S3_ENDPOINT=https://s3.example.com S3_BUCKET=bmd-photos \
    bmd fetch --db bmd.db --max-pages 2 --verbose
`)
}

type fetchCfg struct {
	dataset          string
	dbPath           string
	baseURL          string
	pageSize         int
	startPage        int
	maxPages         int
	delay            time.Duration
	timeout          time.Duration
	cookie           string
	s3endpoint       string
	s3bucket         string
	s3region         string
	s3access         string
	s3secret         string
	s3prefix         string
	s3pathstyle      bool
	s3public         string
	photoConcurrency int
	verbose          bool
}

// getenv checks BMD_ keys first, then legacy OAG_ keys, then default.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if strings.HasPrefix(key, "BMD_") {
		if v := os.Getenv("OAG_"+strings.TrimPrefix(key, "BMD_")); v != "" {
			return v
		}
	}
	return def
}

func getenvRaw(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	c := fetchCfg{}
	fs.StringVar(&c.dataset, "dataset", getenv("BMD_DATASET", "all"), "")
	fs.StringVar(&c.dbPath, "db", getenv("BMD_DB", "bmd.db"), "")
	fs.StringVar(&c.baseURL, "base-url", getenv("BMD_BASE_URL", "https://oag.ecitizen.go.ke"), "")
	fs.IntVar(&c.pageSize, "page-size", 30, "")
	fs.IntVar(&c.startPage, "start-page", 1, "")
	fs.IntVar(&c.maxPages, "max-pages", 0, "")
	fs.DurationVar(&c.delay, "delay", 500*time.Millisecond, "")
	fs.DurationVar(&c.timeout, "timeout", 30*time.Second, "")
	fs.StringVar(&c.cookie, "cookie", getenvRaw("BMD_COOKIE", "OAG_COOKIE"), "")
	fs.StringVar(&c.s3endpoint, "s3-endpoint", os.Getenv("S3_ENDPOINT"), "")
	fs.StringVar(&c.s3bucket, "s3-bucket", os.Getenv("S3_BUCKET"), "")
	fs.StringVar(&c.s3region, "s3-region", getenvRaw("S3_REGION", "AWS_REGION"), "")
	fs.StringVar(&c.s3access, "s3-access-key", getenvRaw("S3_ACCESS_KEY", "AWS_ACCESS_KEY_ID"), "")
	fs.StringVar(&c.s3secret, "s3-secret-key", getenvRaw("S3_SECRET_KEY", "AWS_SECRET_ACCESS_KEY"), "")
	fs.StringVar(&c.s3prefix, "s3-prefix", getenv("S3_PREFIX", "notices"), "")
	fs.BoolVar(&c.s3pathstyle, "s3-path-style", true, "")
	fs.StringVar(&c.s3public, "s3-public-url", os.Getenv("S3_PUBLIC_URL"), "")
	fs.IntVar(&c.photoConcurrency, "photo-concurrency", 4, "")
	fs.BoolVar(&c.verbose, "verbose", getenvRaw("BMD_VERBOSE", "OAG_VERBOSE") != "", "")
	if v := getenv("BMD_PAGE_SIZE", ""); !argsContain(args, "-page-size", "--page-size") && v != "" {
		fmt.Sscanf(v, "%d", &c.pageSize)
	}
	if v := getenv("BMD_START_PAGE", ""); !argsContain(args, "-start-page", "--start-page") && v != "" {
		fmt.Sscanf(v, "%d", &c.startPage)
	}
	if v := getenv("BMD_MAX_PAGES", ""); !argsContain(args, "-max-pages", "--max-pages") && v != "" {
		fmt.Sscanf(v, "%d", &c.maxPages)
	}
	if v := getenv("BMD_DELAY", ""); !argsContain(args, "-delay", "--delay") && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.delay = d
		}
	}
	if v := getenv("BMD_TIMEOUT", ""); !argsContain(args, "-timeout", "--timeout") && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.timeout = d
		}
	}
	if v := os.Getenv("S3_PATH_STYLE"); v == "false" || v == "0" {
		c.s3pathstyle = false
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	var datasets []string
	switch strings.ToLower(strings.TrimSpace(c.dataset)) {
	case "", "all", "both":
		datasets = []string{"marriages", "impediments"}
	case "marriages", "marriage":
		datasets = []string{"marriages"}
	case "impediments", "impediment":
		datasets = []string{"impediments"}
	default:
		return fmt.Errorf("invalid --dataset %q: want marriages|impediments|all", c.dataset)
	}
	if c.pageSize < 1 {
		c.pageSize = 30
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	db, err := store.Open(c.dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite %s: %w", c.dbPath, err)
	}
	defer db.Close()

	client := api.New(c.baseURL, c.timeout)
	client.Cookie = c.cookie
	client.PageSize = c.pageSize

	up, err := s3store.New(s3store.Config{
		Endpoint:  strings.TrimSpace(c.s3endpoint),
		Bucket:    strings.TrimSpace(c.s3bucket),
		Region:    strings.TrimSpace(c.s3region),
		AccessKey: c.s3access,
		SecretKey: c.s3secret,
		Prefix:    c.s3prefix,
		PathStyle: c.s3pathstyle,
		PublicURL: strings.TrimSpace(c.s3public),
	})
	if err != nil {
		return fmt.Errorf("s3 setup: %w", err)
	}
	if up == nil {
		log.Printf("S3 not configured (set S3_ENDPOINT + S3_BUCKET) - storing rows without photo upload")
	} else {
		log.Printf("S3 uploads enabled: endpoint=%s bucket=%s prefix=%s", c.s3endpoint, c.s3bucket, c.s3prefix)
	}

	st, err := runner.Run(ctx, client, db, up, runner.Options{
		Datasets:         datasets,
		PageSize:         c.pageSize,
		StartPage:        c.startPage,
		MaxPages:         c.maxPages,
		RequestDelay:     c.delay,
		PhotoConcurrency: c.photoConcurrency,
		Verbose:          c.verbose,
	})
	if err != nil {
		return err
	}
	log.Printf("done: pages=%d inserted=%d skipped=%d photos_ok=%d photos_failed=%d db=%s",
		st.PagesFetched, st.Inserted, st.Skipped, st.PhotosOK, st.PhotosFailed, c.dbPath)
	return nil
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	dbPath := fs.String("db", getenv("BMD_DB", "bmd.db"), "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	counts, err := db.Counts()
	if err != nil {
		return err
	}
	total := 0
	for ds, n := range counts {
		fmt.Printf("%s: %d\n", ds, n)
		total += n
	}
	fmt.Printf("total: %d (%s)\n", total, *dbPath)
	return nil
}

func argsContain(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}
