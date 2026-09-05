package printrender

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type proxyConfig struct {
	origin        string
	forwardOrigin string
	initialURL    string
	capability    string
	jobID         string
}

type proxyAdmission struct {
	mu          sync.Mutex
	initialUsed bool
	config      proxyConfig
	origin      *url.URL
	forward     *url.URL
}

type attemptProxy struct {
	listener   net.Listener
	server     *http.Server
	transport  proxyTransport
	done       chan error
	handlers   joinGroup
	closeOnce  sync.Once
	closeErr   error
	handlerMu  sync.Mutex
	handlerErr error
	admission  *proxyAdmission
}

type proxyTransport interface {
	http.RoundTripper
	CloseIdleConnections()
}

func startAttemptProxy(ctx context.Context, config proxyConfig) (*attemptProxy, error) {
	origin, err := url.Parse(config.origin)
	if err != nil {
		return nil, err
	}
	forwardRaw := config.forwardOrigin
	if forwardRaw == "" {
		forwardRaw = config.origin
	}
	forward, err := url.Parse(forwardRaw)
	if err != nil || forward.Scheme != "http" || forward.Host == "" || forward.Path != "" {
		return nil, ErrRenderFailed
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	proxy := &attemptProxy{
		listener: listener,
		transport: &http.Transport{
			Proxy:              nil,
			DialContext:        (&net.Dialer{}).DialContext,
			DisableCompression: true,
			ForceAttemptHTTP2:  false,
		},
		done:      make(chan error, 1),
		admission: &proxyAdmission{config: config, origin: origin, forward: forward},
	}
	proxy.server = &http.Server{Handler: http.HandlerFunc(proxy.serveHTTP), ReadHeaderTimeout: 2 * time.Second}
	go func() {
		serveErr := proxy.server.Serve(listener)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		proxy.done <- serveErr
	}()
	return proxy, nil
}

func (p *attemptProxy) url() string { return "http://" + p.listener.Addr().String() }

func (p *attemptProxy) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	if !p.handlers.begin() {
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	defer p.handlers.done()
	initial, allowed := p.admission.admit(request)
	if !allowed {
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	forward := request.Clone(request.Context())
	forward.RequestURI = ""
	forward.URL.Scheme = p.admission.forward.Scheme
	forward.URL.Host = p.admission.forward.Host
	forward.Host = p.admission.origin.Host
	stripAuthority(forward.Header)
	if initial {
		forward.Header.Set("Authorization", "RenderCapability "+p.admission.config.capability)
		forward.Header.Set("X-Render-Job-ID", p.admission.config.jobID)
	}
	response, err := p.transport.RoundTrip(forward)
	if err != nil {
		http.Error(writer, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() {
		p.recordHandlerError(response.Body.Close())
	}()
	for name, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, copyErr := io.Copy(writer, response.Body)
	p.recordHandlerError(copyErr)
}

func (a *proxyAdmission) admit(request *http.Request) (bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if request.Method == http.MethodConnect || request.Method != http.MethodGet || request.ContentLength != 0 || len(request.TransferEncoding) != 0 || request.URL.Scheme != "http" || request.URL.Opaque != "" || request.URL.User != nil || request.URL.Host != a.origin.Host || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.Fragment != "" || request.URL.RawFragment != "" {
		return false, false
	}
	raw := request.URL.String()
	if raw == a.config.initialURL && !a.initialUsed {
		if !exactRequestHeader(request.Header, "Authorization", "RenderCapability "+a.config.capability) || !exactRequestHeader(request.Header, "X-Render-Job-ID", a.config.jobID) {
			return false, false
		}
		a.initialUsed = true
		return true, true
	}
	if !allowedAssetPath(request.URL.Path) || request.URL.RawPath != "" || hasAuthority(request.Header) {
		return false, false
	}
	return false, true
}

func allowedAssetPath(path string) bool {
	if path == "/_nuxt/assets/print.css" || path == "/_nuxt/assets/print-fonts.css" {
		return true
	}
	_, ok := fontPaths[path]
	return ok
}

func exactRequestHeader(header http.Header, name, want string) bool {
	values := header.Values(name)
	return len(values) == 1 && values[0] == want
}

func hasAuthority(header http.Header) bool {
	return len(header.Values("Authorization")) != 0 || len(header.Values("Cookie")) != 0 || len(header.Values("X-Render-Job-ID")) != 0
}

func stripAuthority(header http.Header) {
	for name := range header {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Cookie") || strings.EqualFold(name, "X-Render-Job-ID") || strings.EqualFold(name, "Proxy-Authorization") {
			header.Del(name)
		}
	}
}

func (p *attemptProxy) close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		p.handlers.stop()
		serverErr := p.server.Close()
		listenerErr := p.listener.Close()
		p.transport.CloseIdleConnections()
		p.handlers.wait()
		serveErr := <-p.done
		var closeErrors []error
		if serverErr != nil {
			closeErrors = append(closeErrors, serverErr)
		}
		if listenerErr != nil && !errors.Is(listenerErr, net.ErrClosed) {
			closeErrors = append(closeErrors, listenerErr)
		}
		if serveErr != nil {
			closeErrors = append(closeErrors, serveErr)
		}
		if handlerErr := p.joinedHandlerError(); handlerErr != nil {
			closeErrors = append(closeErrors, handlerErr)
		}
		p.closeErr = errors.Join(closeErrors...)
	})
	return p.closeErr
}

func (p *attemptProxy) recordHandlerError(err error) {
	if err == nil {
		return
	}
	p.handlerMu.Lock()
	p.handlerErr = errors.Join(p.handlerErr, err)
	p.handlerMu.Unlock()
}

func (p *attemptProxy) joinedHandlerError() error {
	p.handlerMu.Lock()
	defer p.handlerMu.Unlock()
	return p.handlerErr
}

type joinGroup struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	closing bool
}

func (g *joinGroup) begin() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return false
	}
	g.wg.Add(1)
	return true
}

func (g *joinGroup) done() { g.wg.Done() }

func (g *joinGroup) goRun(fn func()) bool {
	if !g.begin() {
		return false
	}
	go func() {
		defer g.done()
		fn()
	}()
	return true
}

func (g *joinGroup) stop() {
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()
}

func (g *joinGroup) wait() { g.wg.Wait() }
