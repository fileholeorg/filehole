package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	txttmpl "text/template"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/sys/unix"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/boltdb/bolt"

	"github.com/gabriel-vasile/mimetype"

	"github.com/dustin/go-humanize"
)

func shortID(length int64) string {
	const CHARS = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ123456789"
	ll := len(CHARS)
	b := make([]byte, length)
	rand.Read(b) // generates len(b) random bytes
	for i := int64(0); i < length; i++ {
		b[i] = CHARS[int(b[i])%ll]
	}
	return string(b)
}

var db *bolt.DB

func (fh FileholeServer) GalleryHandler(w http.ResponseWriter, r *http.Request) {
	v := mux.Vars(r)

	w.Write([]byte(`<!DOCTYPE html><html><head><style>body { background-color: black; color: white; }</style></head><body>`))

	for _, i := range strings.Split(v["files"], ",") {
		link := fh.PublicUrl.String() + `/u/` + i
		w.Write([]byte(`<p>` + html.EscapeString(i) + `</p><a href="` + html.EscapeString(link) + `">` + `<img width=500em src="` + html.EscapeString(link) + `"></img></a>`))
	}

	w.Write([]byte(`</body></html>`))
}

func (fh FileholeServer) UploadHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, fh.UploadLimit) // Make sure we don't fuck up and read too much

	multipReader, err := r.MultipartReader()

	var UploadProperties struct {
		MimeType *mimetype.MIME
		Filename string
		TempFile string
		Expiry   int64
		UrlLen   int64
	}

	// Our defaults
	UploadProperties.Expiry = 86400
	UploadProperties.UrlLen = 24

	parts := 0

	shouldUpload := false

	for {
		parts += 1
		if parts > 55 {
			log.Debug().Err(err).Msg("too many parts in multipart form")
			http.Error(w, "too many parts in multipart form", http.StatusBadRequest)
			return
		}
		if p, err := multipReader.NextPart(); errors.Is(err, io.EOF) {
			log.Debug().Msg("iterated all parts successfully")
			break
		} else if err != nil {
			log.Debug().Err(err).Msg("other error in getting next part of multipart")
			shouldUpload = false
			break
		} else {
			log.Debug().Str("filename", p.FileName()).Str("formname", p.FormName()).Msg("multipReader next")
			switch p.FormName() {
			case "url_len":
				if urlLenBytes, err := io.ReadAll(io.LimitReader(p, 55)); err != nil {
					log.Debug().Err(err).Msg("Error reading url_len bytes")
					break
				} else {
					// url_len sanitize
					inpUrlLen := string(urlLenBytes)
					log.Debug().Str("inpUrlLen", inpUrlLen).Send()
					UploadProperties.UrlLen, err = strconv.ParseInt(inpUrlLen, 10, 64)
					if err != nil {
						log.Debug().Err(err).Msg("Error getting url length")
						UploadProperties.UrlLen = 24
					}
					if UploadProperties.UrlLen < 5 || UploadProperties.UrlLen > 236 {
						w.Write([]byte("url_len needs to be between 5 and 236\n"))
						return
					}
				}

			case "expiry":
				if expiryBytes, err := io.ReadAll(io.LimitReader(p, 55)); err != nil {
					log.Debug().Err(err).Msg("Error reading expiry bytes")
					break
				} else {
					inpExpiry := string(expiryBytes)
					UploadProperties.Expiry, err = strconv.ParseInt(inpExpiry, 10, 64)
					if err != nil {
						UploadProperties.Expiry = 86400
					}
					if UploadProperties.Expiry < 5 || UploadProperties.Expiry > 432000 {
						w.Write([]byte("expiry needs to be between 5 and 432000\n"))
						return
					}
				}

			case "file":
				fuckYou := make([]byte, 512)
				n, err := p.Read(fuckYou)
				if n < 512 {
					// really small file, don't make an error, but don't allow it to read into the uninitialized part of the buffer
					fuckYou = fuckYou[0:n]
				} else if err != nil {
					http.Error(w, "error detecting the mime type of your file", http.StatusInternalServerError)
					return
				}

				UploadProperties.MimeType = mimetype.Detect(fuckYou)
				log.Info().Stringer("mtype", UploadProperties.MimeType).Msg("Detected mime type")

				tempFile, err := os.CreateTemp(fh.BufferDir, "")
				if err != nil {
					log.Debug().Err(err).Msg("failed to create temp file for buffering upload")
					http.Error(w, "internal server error", http.StatusInternalServerError)
					return
				}

				UploadProperties.TempFile = tempFile.Name()

				if n, err := io.Copy(tempFile, bytes.NewReader(fuckYou)); err != nil {
					log.Debug().Str("tempFile", tempFile.Name()).Int64("n", n).Msg("failed to copy mime portion of file to disk")
					http.Error(w, "internal server error", http.StatusInternalServerError)
					os.Remove(tempFile.Name())
					return
				}

				if n, err := io.Copy(tempFile, p); err != nil {
					log.Debug().Str("tempFile", tempFile.Name()).Int64("n", n).Msg("failed to copy rest of file to disk")
					// We don't return this error on purpose, for < 512b files
				}

				shouldUpload = true
			default:
				break
			}
		}
	}

	if shouldUpload {
		name := shortID(UploadProperties.UrlLen) + UploadProperties.MimeType.Extension()

		newName := fh.StorageDir + "/" + name

		if err := os.Rename(UploadProperties.TempFile, newName); err != nil {
			log.Debug().Err(err).Str("oldName", UploadProperties.TempFile).Str("newName", newName).Msg("Error moving file from buffer folder")
		}

		if err = db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("expiry"))
			return b.Put([]byte(name), []byte(strconv.FormatInt(time.Now().Unix()+UploadProperties.Expiry, 10)))
		}); err != nil {
			log.Error().Err(err).Msg("Failed to put expiry")
		}

		outUrl := fh.PublicUrl.String()
		if fh.ServeUrl != "" {
			outUrl = fh.ServeUrl
		}
		w.Write([]byte(outUrl + "/u/" + name + "\n"))
	} else {
		http.Error(w, "partial upload - perhaps exceeded size limit", http.StatusInternalServerError)
		log.Debug().Msg("shouldUpload was not flagged, partial upload maybe")
	}
}

//go:embed assets/*
var assetsFs embed.FS

type FileholeServer struct {
	Bind         string
	MetadataFile string
	StorageDir   string
	BufferDir    string
	ServeUrl     string
	SiteName     string
	OtherHoles   string
	OtherHole
	UploadLimit int64
	Debug       bool
	CSPDisabled bool
}

func (fh *FileholeServer) CSPMiddleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set(`Permissions-Policy`, `geolocation=(), camera=(), microphone=(), interest-cohort=()`)
			w.Header().Set(`X-Frame-Options`, `DENY`)
			w.Header().Set(`X-Content-Type-Options`, `nosniff`)
			w.Header().Set(`Referrer-Policy`, `no-referrer`)

			if !fh.CSPDisabled {
				cspNonce := shortID(32)
				c := context.WithValue(req.Context(), "csp-nonce", cspNonce)

				csp := `default-src 'none'; `
				csp += `script-src 'nonce-` + cspNonce + `'; `
				csp += `style-src 'nonce-` + cspNonce + `'; `
				csp += `connect-src 'self'; img-src 'self' data:; manifest-src 'self'; media-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none';`

				w.Header().Set(`Content-Security-Policy`, csp)

				next.ServeHTTP(w, req.WithContext(c))
				return
			}

			next.ServeHTTP(w, req)
		})
	}
}

func (fh FileholeServer) getOwnOtherHole() *OtherHole {
	return &OtherHole{
		PublicUrl:        fh.PublicUrl,
		UpstreamProvider: fh.UpstreamProvider,
		Region:           fh.Region,
		Country:          fh.Country,
		Nickname:         fh.Nickname,
		FreeBytes:        fh.FreeBytes,
	}
}

func (fh FileholeServer) HolesHandler(w http.ResponseWriter, r *http.Request) {
	resp := map[string]*OtherHole{}
	resp[fh.PublicUrl.String()] = fh.getOwnOtherHole()

	for _, h := range otherHoles {
		resp[h.PublicUrl.String()] = h
	}

	infoResp, err := json.Marshal(resp)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal holes response")
	}

	w.Header().Add("Content-Type", "application/json")
	w.Write(infoResp)
}

// Periodically update the amount of space left on the storage dir disk
func (fh *FileholeServer) RefreshFreeBytes() {
	for {
		var stat unix.Statfs_t
		unix.Statfs(fh.StorageDir, &stat)

		// Available blocks * size per block = available space in bytes
		fh.FreeBytes = (stat.Bavail * uint64(stat.Bsize))
		time.Sleep(5 * time.Minute)
	}
}

func (fh FileholeServer) InfoHandler(w http.ResponseWriter, r *http.Request) {
	resp := fh.getOwnOtherHole()

	infoResp, err := json.Marshal(&resp)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal info response")
	}

	w.Header().Add("Content-Type", "application/json")
	w.Write(infoResp)
}

// An alternative hole
type OtherHole struct {
	PublicUrl        *url.URL
	Nickname         string
	UpstreamProvider string
	Region           string
	Country          string
	FreeBytes        uint64
}

func RefreshInfo(o *OtherHole) error {
	resp, err := http.Get(o.PublicUrl.JoinPath("/info").String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	m, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(m, o); err != nil {
		return err
	}

	log.Info().Stringer("PublicUrl", o.PublicUrl).Uint64("free", o.FreeBytes).Str("provider", o.UpstreamProvider).Str("country", o.Country).Str("region", o.Region).Msg("Refreshed info from other hole")

	return nil
}

var otherHoles = []*OtherHole{}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	getEnv := func(key string, fallback string) string {
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
		return fallback
	}

	fh := FileholeServer{}
	go fh.RefreshFreeBytes()

	fhPublicUrlDefault := getEnv("FH_PUBLIC_URL", "https://filehole.org")

	flag.StringVar(&fh.Bind, "bind", getEnv("FH_BIND", "127.0.0.1:8000"), "Address to bind ENV: FH_BIND")
	flag.StringVar(&fh.MetadataFile, "metadata-path", getEnv("FH_METADATA_FILE", "./filehole.db"), "File metadata storage KV store filename ENV: FH_METADATA_FILE")
	flag.StringVar(&fh.StorageDir, "storage-dir", getEnv("FH_STORAGE_DIR", "./data"), "Data storage folder ENV: FH_STORAGE_DIR")
	flag.StringVar(&fh.BufferDir, "buffer-dir", getEnv("FH_BUFFER_DIR", "./buffer"), "Buffer folder for uploads ENV: FH_STORAGE_DIR")
	flag.StringVar(&fh.SiteName, "site-name", getEnv("FH_SITE_NAME", "Filehole"), "User facing website branding ENV: FH_SITE_NAME")
	flag.StringVar(&fh.UpstreamProvider, "upstream-provider", getEnv("FH_UPSTREAM_PROVIDER", ""), "User facing upstream provider i.e. AWS ENV: FH_UPSTREAM_PROVIDER")
	flag.StringVar(&fh.Nickname, "nickname", getEnv("FH_NICKNAME", ""), "User facing name of the server i.e. Filehole ENV: FH_NICKNAME")
	flag.StringVar(&fh.Region, "region", getEnv("FH_SITE_REGION", ""), "User facing region i.e. us-east-1 ENV: FH_SITE_REGION")
	flag.StringVar(&fh.Country, "country", getEnv("FH_SITE_COUNTRY", ""), "ISO 3166 country code i.e. US ENV: FH_SITE_COUNTRY")
	flag.StringVar(&fh.OtherHoles, "other-holes", getEnv("FH_OTHER_HOLES", ""), "Alternative holes as a comma separated list ENV: FH_OTHER_HOLES")
	flag.StringVar(&fh.ServeUrl, "serve-url", getEnv("FH_SERVE_URL", fhPublicUrlDefault), "Internet facing URL of the base of uploads, only for using a CDN, object storage, etc. ENV: FH_SERVE_URL")

	fh.Debug = os.Getenv("FH_DEBUG") != ""
	flag.BoolVar(&fh.Debug, "debug", fh.Debug, "Enable debug logging for development ENV: FH_DEBUG")

	fh.CSPDisabled = os.Getenv("FH_CSP_OFF") != ""
	flag.BoolVar(&fh.CSPDisabled, "csp-off", fh.CSPDisabled, "Disable Content-Security-Policy nonces ENV: FH_CSP_OFF")

	pubUrl := ""
	flag.StringVar(&pubUrl, "public-url", getEnv("FH_PUBLIC_URL", fhPublicUrlDefault), "Internet facing URL of the base of the site ENV: FH_PUBLIC_URL")

	const DEFAULT_UPLOAD_LIMIT = 1024 * 1024 * 1024

	if env_fh_upload_limit, exists := os.LookupEnv("FH_UPLOAD_LIMIT"); exists {
		var err error
		if fh.UploadLimit, err = strconv.ParseInt(env_fh_upload_limit, 10, 64); err != nil {
			log.Error().Err(err).Msg("Could not parse FH_UPLOAD_LIMIT as a uint64. Defaulting to 1GiB.")
			fh.UploadLimit = DEFAULT_UPLOAD_LIMIT
		}
	} else {
		fh.UploadLimit = DEFAULT_UPLOAD_LIMIT
	}

	flag.Int64Var(&fh.UploadLimit, "upload-limit", fh.UploadLimit, "Max allowed size for a HTTP request in bytes ENV: FH_UPLOAD_LIMIT")

	flag.Parse()

	if fh.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Warn().Msg("Debug logging is enabled")
	}

	// Verify the Public URL
	if u, err := url.Parse(pubUrl); err != nil {
		log.Fatal().Err(err).Msg("failed to parse public url")
	} else {
		fh.PublicUrl = u
	}

	// If you only want the one hole
	if fh.OtherHoles != "" {
		for _, altUrl := range strings.Split(fh.OtherHoles, ",") {
			u, err := url.Parse(altUrl)
			if err != nil {
				log.Fatal().Err(err).Msg("failed to parse other hole url")
			}

			otherHoles = append(otherHoles, &OtherHole{
				PublicUrl: u,
			})
		}
	}

	// Refresh the other hole's info in the background every 5 minutes
	go func() {
		for {
			for _, otherHole := range otherHoles {
				if err := RefreshInfo(otherHole); err != nil {
					log.Error().Err(err).Stringer("url", otherHole.PublicUrl).Msg("failed to refresh info for other hole")
				}
			}
			time.Sleep(5 * time.Minute)
		}
	}()

	var err error
	db, err = bolt.Open(fh.MetadataFile, 0600, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("dangerous database activity")
	}
	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("expiry"))
		if err != nil {
			log.Fatal().Err(err).Msg("Error creating expiry bucket")
			return err
		}
		return nil
	})

	// Directories should already exist, we will try to make them
	if err := os.MkdirAll(fh.StorageDir, os.ModePerm); !errors.Is(err, os.ErrExist) {
		log.Error().Err(err).Msg("Failed to create storage directory")
	}

	if err := os.MkdirAll(fh.BufferDir, os.ModePerm); !errors.Is(err, os.ErrExist) {
		log.Error().Err(err).Msg("Failed to create buffer directory")
	}
	r := mux.NewRouter()

	r.Use(fh.CSPMiddleware())

	// Serve multiple images in a gallery
	r.HandleFunc("/g/{files}", fh.GalleryHandler)

	// Serve files from data dir statically
	r.PathPrefix("/u/").Handler(http.StripPrefix("/u/", NoDirectoryList(http.FileServer(http.Dir(fh.StorageDir)))))

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		indexPage, err := assetsFs.ReadFile("assets/index.html")
		if err != nil {
			log.Error().Err(err).Msg("failed to retrieve index.html")
		}
		t, err := template.New("").Funcs(template.FuncMap{
			"ToLower":       strings.ToLower,
			"HumanizeBytes": humanize.IBytes,
		}).Parse(string(indexPage))
		if err != nil {
			log.Error().Err(err).Msg("failed to parse index.html template")
		}

		if err := t.Execute(w, map[string]interface{}{
			"PublicUrl":        fh.PublicUrl.String(),
			"SiteName":         fh.SiteName,
			"Nickname":         fh.Nickname,
			"Region":           fh.Region,
			"Country":          fh.Country,
			"UpstreamProvider": fh.UpstreamProvider,
			"Debug":            fh.Debug,
			"FreeBytes":        fh.FreeBytes,
			"CSPNonce":         r.Context().Value("csp-nonce"),
			"OtherHoles":       otherHoles,
		}); err != nil {
			log.Error().Err(err).Msg("failed to render template")
		}
	}).Methods("GET")

	serveAsset := func(w http.ResponseWriter, path string, contentType string) {
		w.Header().Add("Content-Type", contentType)
		assetBytes, err := assetsFs.ReadFile(path)
		if err != nil {
			log.Error().Err(err).Str("path", path).Msg("failed to retrieve")
		}
		w.Write(assetBytes)
	}

	r.HandleFunc("/asset/country-flag.css", func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, "assets/country-flag.css", "text/css")
	}).Methods("GET")

	r.HandleFunc("/asset/country-flag.js", func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, "assets/country-flag.js", "text/javascript")
	}).Methods("GET")

	r.HandleFunc("/asset/country-flag.png", func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, "assets/country-flag.png", "image/png")
	}).Methods("GET")

	r.HandleFunc("/asset/filehole.css", func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, "assets/filehole.css", "text/css")
	}).Methods("GET")

	r.HandleFunc("/asset/pico.min.css", func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, "assets/pico.min.css", "text/css")
	}).Methods("GET")

	r.HandleFunc("/asset/jquery-3.7.1.min.js", func(w http.ResponseWriter, _ *http.Request) {
		serveAsset(w, "assets/jquery-3.7.1.min.js", "text/javascript")
	}).Methods("GET")

	r.HandleFunc("/asset/filehole.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Type", "text/javascript")

		frontendJs, err := assetsFs.ReadFile("assets/filehole.js")
		if err != nil {
			log.Error().Err(err).Msg("failed to retrieve filehole.js")
		}
		t, _ := txttmpl.New("fileholejs").Parse(string(frontendJs))

		t.Execute(w, map[string]interface{}{
			"PublicUrl": fh.PublicUrl.String(),
			"SiteName":  fh.SiteName,
			"Debug":     fh.Debug,
		})
	}).Methods("GET")

	r.HandleFunc("/info", fh.InfoHandler).Methods("GET")

	r.HandleFunc("/holes", fh.HolesHandler).Methods("GET")

	r.HandleFunc("/", fh.UploadHandler).Methods("POST")

	http.Handle("/", r)

	go fh.ExpiryDoer()

	http.ListenAndServe(fh.Bind, r)

	db.Close()
}
