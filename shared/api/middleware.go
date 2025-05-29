// shared/api/middleware.go (Revised)
package api

import (
	// Or a custom logger interface
	"net/http"
)

// LoggingMiddleware logs details of each HTTP request.
// It's good practice to pass a logger into middleware rather than relying on global log.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//start := time.Now()
		// Wrap the ResponseWriter to capture status code
		lrw := &loggingResponseWriter{w: w}
		next.ServeHTTP(lrw, r)

		//log.Printf("INFO: %s %s from %s - Status: %d, Duration: %v",	r.Method, r.URL.Path, r.RemoteAddr, lrw.statusCode, time.Since(start))
	})
}

// loggingResponseWriter is a wrapper to capture the HTTP status code.
type loggingResponseWriter struct {
	w          http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) Header() http.Header {
	return lrw.w.Header()
}

func (lrw *loggingResponseWriter) Write(buf []byte) (int, error) {
	return lrw.w.Write(buf)
}

func (lrw *loggingResponseWriter) WriteHeader(statusCode int) {
	lrw.statusCode = statusCode
	lrw.w.WriteHeader(statusCode)
}

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400") // Cache preflight requests for 24 hours

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
