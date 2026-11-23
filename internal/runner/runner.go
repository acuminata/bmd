package runner

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"bmd/internal/api"
	"bmd/internal/model"
	"bmd/internal/s3store"
	"bmd/internal/store"
)

// Options controls a fetch run.
type Options struct {
	Datasets         []string
	PageSize         int
	StartPage        int
	MaxPages         int // 0 = all pages
	RequestDelay     time.Duration
	PhotoConcurrency int
	Verbose          bool
}

// Stats tallies a run.
type Stats struct {
	PagesFetched int
	Inserted     int
	Skipped      int
	PhotosOK     int
	PhotosFailed int
}

// Run crawls each dataset page-by-page, inserts new entries
// (INSERT OR IGNORE on application_number), and uploads the two
// original photos to S3 for newly inserted rows only.
func Run(ctx context.Context, client *api.Client, db *store.DB, up *s3store.Uploader, opt Options) (Stats, error) {
	var st Stats
	if opt.PageSize < 1 {
		opt.PageSize = 30
	}
	if opt.StartPage < 1 {
		opt.StartPage = 1
	}
	if opt.PhotoConcurrency < 1 {
		opt.PhotoConcurrency = 4
	}
	sem := make(chan struct{}, opt.PhotoConcurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for _, dataset := range opt.Datasets {
		page := opt.StartPage
		pagesDone := 0
		for {
			if opt.MaxPages > 0 && pagesDone >= opt.MaxPages {
				break
			}
			select {
			case <-ctx.Done():
				return st, ctx.Err()
			default:
			}
			resp, err := client.FetchPage(ctx, dataset, page, opt.PageSize)
			if err != nil {
				return st, err
			}
			st.PagesFetched++
			if opt.Verbose {
				log.Printf("%s: page %d/%d (%d entries, total %d)", dataset, resp.PageNumber, resp.TotalPages, len(resp.Entries), resp.TotalEntries)
			} else {
				log.Printf("%s: page %d/%d: %d entries", dataset, resp.PageNumber, resp.TotalPages, len(resp.Entries))
			}
			for _, e := range resp.Entries {
				ok, err := db.InsertIgnore(dataset, e)
				if err != nil {
					return st, fmt.Errorf("insert %s %s: %w", dataset, e.ApplicationNumber, err)
				}
				if !ok {
					st.Skipped++
					continue
				}
				st.Inserted++
				if up == nil {
					continue
				}
				wg.Add(1)
				sem <- struct{}{}
				go func(ent model.Entry) {
					defer wg.Done()
					defer func() { <-sem }()
					groomURL, brideURL := uploadPhotos(ctx, client, up, dataset, ent)
					mu.Lock()
					defer mu.Unlock()
					if groomURL == "" && brideURL == "" && (ent.BrideGroomsPhoto != "" || ent.BridesPhoto != "") {
						st.PhotosFailed++
					} else {
						st.PhotosOK++
					}
					if err := db.UpdatePhotos(dataset, ent.ApplicationNumber, groomURL, brideURL); err != nil {
						log.Printf("update photos %s: %v", ent.ApplicationNumber, err)
						if firstErr == nil {
							firstErr = err
						}
					}
				}(e)
			}
			pagesDone++
			if resp.TotalPages > 0 && resp.PageNumber >= resp.TotalPages {
				break
			}
			if len(resp.Entries) == 0 {
				break
			}
			page = resp.PageNumber + 1
			if opt.RequestDelay > 0 {
				select {
				case <-ctx.Done():
					return st, ctx.Err()
				case <-time.After(opt.RequestDelay):
				}
			}
		}
	}
	wg.Wait()
	return st, firstErr
}

func uploadPhotos(ctx context.Context, client *api.Client, up *s3store.Uploader, dataset string, e model.Entry) (groomURL, brideURL string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if e.BrideGroomsPhoto != "" {
		if data, ct, err := client.Download(ctx, e.BrideGroomsPhoto); err != nil {
			log.Printf("photo download groom %s: %v", e.ApplicationNumber, err)
		} else {
			key := up.KeyFor(dataset, e.ApplicationNumber, "groom", e.BrideGroomsPhoto, ct)
			if stored, err := up.Put(ctx, key, data, contentTypeOr(ct, data)); err != nil {
				log.Printf("photo upload groom %s: %v", e.ApplicationNumber, err)
			} else {
				groomURL = stored
			}
		}
	}
	if e.BridesPhoto != "" {
		if data, ct, err := client.Download(ctx, e.BridesPhoto); err != nil {
			log.Printf("photo download bride %s: %v", e.ApplicationNumber, err)
		} else {
			key := up.KeyFor(dataset, e.ApplicationNumber, "bride", e.BridesPhoto, ct)
			if stored, err := up.Put(ctx, key, data, contentTypeOr(ct, data)); err != nil {
				log.Printf("photo upload bride %s: %v", e.ApplicationNumber, err)
			} else {
				brideURL = stored
			}
		}
	}
	return groomURL, brideURL
}

func contentTypeOr(ct string, data []byte) string {
	if ct != "" {
		return ct
	}
	if len(data) >= 512 {
		return sniff(data[:512])
	}
	return sniff(data)
}

func sniff(b []byte) string {
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return "image/jpeg"
	}
	if len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return "image/png"
	}
	if len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a") {
		return "image/gif"
	}
	return "application/octet-stream"
}
