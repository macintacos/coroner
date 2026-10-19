package export

import (
	"slices"
	"strings"
)

// AttachmentPath returns the path, relative to an export's directory, that a's content is
// written to beside the export: attachments/<sha256><extension>, with the extension of a's
// media type, or .bin for a type it has none for. a.SHA256 must be 64 lowercase hex digits,
// as Decode and the store ensure; then no text a person typed reaches the path.
func AttachmentPath(a Attachment) string {
	return "attachments/" + a.SHA256 + extension(mediaType(a))
}

func mediaType(a Attachment) string {
	mediaType, _, _ := strings.Cut(a.MIME, ";")

	return strings.TrimSpace(mediaType)
}

// rasterImage reports whether a is an image a reader can show inline without running it.
func rasterImage(a Attachment) bool {
	return slices.Contains([]string{".bmp", ".gif", ".jpg", ".png", ".webp", ".ico"}, extension(mediaType(a)))
}

// extension is fixed, rather than mime.ExtensionsByType, which reads the host's mime.types
// files and would name the same attachment differently on another machine. It covers what
// http.DetectContentType and store.DetectMIME report. HTML, XML and SVG are .txt so a host
// serving the export does not run them as pages.
func extension(mediaType string) string {
	switch mediaType {
	case "application/json":
		return ".json"
	case "application/ogg":
		return ".ogg"
	case "application/pdf":
		return ".pdf"
	case "application/postscript":
		return ".ps"
	case "application/vnd.ms-fontobject":
		return ".eot"
	case "application/wasm":
		return ".wasm"
	case "application/x-7z-compressed":
		return ".7z"
	case "application/x-gzip":
		return ".gz"
	case "application/x-rar-compressed":
		return ".rar"
	case "application/yaml":
		return ".yaml"
	case "application/zip":
		return ".zip"
	case "audio/aiff":
		return ".aiff"
	case "audio/basic":
		return ".au"
	case "audio/midi":
		return ".mid"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wave":
		return ".wav"
	case "font/collection":
		return ".ttc"
	case "font/otf":
		return ".otf"
	case "font/ttf":
		return ".ttf"
	case "font/woff":
		return ".woff"
	case "font/woff2":
		return ".woff2"
	case "image/bmp":
		return ".bmp"
	case "image/gif":
		return ".gif"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/x-icon":
		return ".ico"
	case "text/csv":
		return ".csv"
	case "text/markdown":
		return ".md"
	case "text/plain", "text/html", "text/xml", "image/svg+xml":
		return ".txt"
	case "video/avi":
		return ".avi"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	}

	return ".bin"
}
