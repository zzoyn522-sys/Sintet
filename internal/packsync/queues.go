package packsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

const DefaultCDNWorkers = 6
const DefaultP2PWorkers = 2

var ErrPeerFilesUnavailable = errors.New("соединение с хостом недоступно")

type DownloadOptions struct {
	HTTP                   *http.Client
	Resolvers              []Resolver
	CDNWorkers, P2PWorkers int
}

func Download(ctx context.Context, m Manifest, cache string, peer Fetcher, local map[string]string, progress func(string, int64, int64)) error {
	return DownloadWithOptions(ctx, m, cache, peer, local, progress, DownloadOptions{})
}
func DownloadWithOptions(ctx context.Context, m Manifest, cache string, peer Fetcher, local map[string]string, progress func(string, int64, int64), options DownloadOptions) error {
	if err := Validate(m); err != nil {
		return err
	}
	jobs := []File{}
	var total int64
	seen := map[string]bool{}
	for _, file := range m.Files {
		if seen[file.SHA256] {
			continue
		}
		seen[file.SHA256] = true
		if !ValidCached(cache, file) {
			jobs = append(jobs, file)
			total += file.Size
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	resolvers := options.Resolvers
	if resolvers == nil {
		resolvers = []Resolver{ModrinthResolver{}}
	}
	unresolved := []File{}
	for _, file := range jobs {
		if local[file.SHA256] == "" {
			unresolved = append(unresolved, file)
		}
	}
	sources := ResolveSources(ctx, unresolved, resolvers)
	cdnWorkers, p2pWorkers := options.CDNWorkers, options.P2PWorkers
	if cdnWorkers == 0 {
		cdnWorkers = DefaultCDNWorkers
	}
	if p2pWorkers == 0 {
		p2pWorkers = DefaultP2PWorkers
	}
	cdnWorkers = max(1, min(8, cdnWorkers))
	p2pWorkers = max(1, min(2, p2pWorkers))
	cdnQueue, p2pQueue := make(chan File, len(jobs)), make(chan File, len(jobs))
	for _, file := range jobs {
		if local[file.SHA256] != "" || sources[file.SHA256].URL != "" {
			cdnQueue <- file
		} else {
			p2pQueue <- file
		}
	}
	close(cdnQueue)
	var progressMu sync.Mutex
	var done int64
	report := func(file string, n int64) {
		progressMu.Lock()
		defer progressMu.Unlock()
		done += n
		if progress != nil {
			progress(file, done, total)
		}
	}
	failures := make(chan error, 1)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	downloadCDN := func(file File, source Source) bool {
		if !validSource(source) {
			return false
		}
		request, err := http.NewRequestWithContext(ctx, "GET", source.URL, nil)
		if err != nil {
			return false
		}
		request.Header.Set("User-Agent", "Sintet/0.8.9.3 (pack sync; sintet.xyz)")
		client := &http.Client{}
		if options.HTTP != nil {
			copy := *options.HTTP
			client = &copy
		}
		client.Timeout = 2 * time.Minute
		client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if len(via) > 4 || !validSource(Source{source.Provider, next.URL.String()}) {
				return errors.New("untrusted CDN redirect")
			}
			next.Header.Del("Authorization")
			next.Header.Del("x-api-key")
			next.Header.Del("Cookie")
			return nil
		}
		response, err := client.Do(request)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return false
		}
		var transferred int64
		err = CopyVerified(ctx, response.Body, cache, file, func(n int64) { transferred += n; report(file.Path, n) })
		if err != nil {
			if transferred > 0 {
				report(file.Path, -transferred)
			}
			return false
		}
		return true
	}
	var cdnWait, p2pWait sync.WaitGroup
	for i := 0; i < p2pWorkers; i++ {
		p2pWait.Add(1)
		go func() {
			defer p2pWait.Done()
			for file := range p2pQueue {
				if ctx.Err() != nil {
					return
				}
				if err := downloadPeerFile(ctx, peer, cache, file, report); err != nil {
					fail(err)
					return
				}
			}
		}()
	}
	for i := 0; i < cdnWorkers; i++ {
		cdnWait.Add(1)
		go func() {
			defer cdnWait.Done()
			for file := range cdnQueue {
				if ctx.Err() != nil {
					return
				}
				report(file.Path, 0)
				present := false
				if source := local[file.SHA256]; source != "" {
					if input, err := os.Open(source); err == nil {
						err = CopyVerified(ctx, input, cache, file, nil)
						input.Close()
						if err == nil {
							report(file.Path, file.Size)
							present = true
						}
					}
				}
				if !present && sources[file.SHA256].URL != "" {
					present = downloadCDN(file, sources[file.SHA256])
				}
				if !present {
					select {
					case p2pQueue <- file:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	cdnWait.Wait()
	close(p2pQueue)
	p2pWait.Wait()
	select {
	case err := <-failures:
		return err
	default:
		return ctx.Err()
	}
}
func downloadPeerFile(ctx context.Context, peer Fetcher, cache string, file File, report func(string, int64)) error {
	if peer == nil {
		return ErrPeerFilesUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	report(file.Path, 0)
	var err error
	for attempt := range 3 {
		var retry bool
		var transferred int64
		response, fetchErr := peer.PackGet(ctx, "/file/"+file.SHA256)
		switch {
		case fetchErr != nil:
			err = fetchErr
			retry = !errors.Is(err, net.ErrClosed)
		case response.StatusCode != http.StatusOK:
			response.Body.Close()
			code := response.StatusCode
			err = fmt.Errorf("хост не отдал файл %s: HTTP %d", file.Path, code)
			retry = code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500 && code <= 599
		default:
			err = CopyVerified(ctx, response.Body, cache, file, func(n int64) {
				transferred += n
				report(file.Path, n)
			})
			response.Body.Close()
			if err == nil {
				return nil
			}
			var network net.Error
			retry = errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &network)
		}
		if transferred > 0 {
			report(file.Path, -transferred)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !retry || attempt == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return err
}
