package export

import "strings"

// AttachmentFile returns the path, relative to an export's directory, that a's content is
// written to beside the export: attachments/<sha256><extension>, with the extension of a's
// media type, or .bin for a type it has none for. Only the hash names the file, so no text a
// person typed reaches the path.
func AttachmentFile(a Attachment) string {
	mediaType, _, _ := strings.Cut(a.MIME, ";")
	extension, ok := extensions()[strings.TrimSpace(mediaType)]
	if !ok {
		extension = ".bin"
	}

	return "attachments/" + a.SHA256 + extension
}

// extensions is fixed, rather than mime.ExtensionsByType, which reads the host's mime.types
// files and would name the same attachment differently on another machine. It covers what
// http.DetectContentType and store.DetectMIME report.
func extensions() map[string]string {
	return map[string]string{
		"application/json":              ".json",
		"application/ogg":               ".ogg",
		"application/pdf":               ".pdf",
		"application/postscript":        ".ps",
		"application/vnd.ms-fontobject": ".eot",
		"application/wasm":              ".wasm",
		"application/x-7z-compressed":   ".7z",
		"application/x-gzip":            ".gz",
		"application/x-rar-compressed":  ".rar",
		"application/yaml":              ".yaml",
		"application/zip":               ".zip",
		"audio/aiff":                    ".aiff",
		"audio/basic":                   ".au",
		"audio/midi":                    ".mid",
		"audio/mpeg":                    ".mp3",
		"audio/wave":                    ".wav",
		"font/collection":               ".ttc",
		"font/otf":                      ".otf",
		"font/ttf":                      ".ttf",
		"font/woff":                     ".woff",
		"font/woff2":                    ".woff2",
		"image/bmp":                     ".bmp",
		"image/gif":                     ".gif",
		"image/jpeg":                    ".jpg",
		"image/png":                     ".png",
		"image/svg+xml":                 ".svg",
		"image/webp":                    ".webp",
		"image/x-icon":                  ".ico",
		"text/csv":                      ".csv",
		"text/html":                     ".html",
		"text/markdown":                 ".md",
		"text/plain":                    ".txt",
		"text/xml":                      ".xml",
		"video/avi":                     ".avi",
		"video/mp4":                     ".mp4",
		"video/webm":                    ".webm",
	}
}
