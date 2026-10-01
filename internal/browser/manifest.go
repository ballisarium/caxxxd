package browser

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func cleanCodecs(value string) string {
	if len(value) > 96 {
		return ""
	}
	value = strings.ReplaceAll(value, " ", "")
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".,_-", c)) {
			return ""
		}
	}
	return value
}

const (
	maxManifestBodyBytes = 2 << 20
	maxManifestLineBytes = 64 << 10
	maxManifestLines     = 50000
	maxManifestVariants  = 256
)

var isoDurationPattern = regexp.MustCompile(`^P(?:(\d+(?:\.\d+)?)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

type manifestEstimate struct {
	DurationSeconds float64
	Bytes           int64
	Exact           bool
	Variants        []manifestVariant
	Width, Height   int
	Codecs          string
}

type manifestVariant struct {
	URL           string
	Bandwidth     int64
	Width, Height int
	Codecs        string
}

// manifestSize extracts finite duration, size, and variant information from a
// bounded HLS or DASH manifest. Invalid, unsupported, or live manifests return
// whichever finite metadata can be established safely, or an empty estimate.
func manifestSize(rawURL, kind, body string) manifestEstimate {
	if !ValidURL(rawURL) || len(body) == 0 || len(body) > maxManifestBodyBytes {
		return manifestEstimate{}
	}
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "HLS":
		return hlsManifestSize(rawURL, body)
	case "DASH":
		return dashManifestSize(body)
	default:
		return manifestEstimate{}
	}
}

func hlsManifestSize(rawURL, body string) manifestEstimate {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 4096), maxManifestLineBytes)
	lineCount := 0
	headerSeen := false
	endList := false
	var pendingDuration float64
	durationPending := false
	durationValid := true
	durationTotal := float64(0)
	var pendingRange int64
	rangePending := false
	rangeValid := true
	allSegmentsRanged := true
	segmentCount := 0
	master := false
	var pendingVariant map[string]string
	result := manifestEstimate{}

	for scanner.Scan() {
		lineCount++
		if lineCount > maxManifestLines {
			return manifestEstimate{}
		}
		line := strings.TrimSpace(scanner.Text())
		if lineCount == 1 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if line == "" {
			continue
		}
		if !headerSeen {
			if line != "#EXTM3U" {
				return manifestEstimate{}
			}
			headerSeen = true
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			master = true
			pendingVariant = parseHLSAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			if durationPending {
				durationValid = false
			}
			value := strings.TrimPrefix(line, "#EXTINF:")
			value, _, _ = strings.Cut(value, ",")
			pendingDuration, durationPending = parsePositiveOrZeroFloat(value)
			if !durationPending {
				durationValid = false
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-BYTERANGE:") {
			if rangePending || !rangeValid {
				pendingRange = 0
				rangePending = false
				rangeValid = false
				continue
			}
			pendingRange, rangePending = parseHLSByteRange(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"))
			if !rangePending {
				rangeValid = false
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			// Initialization sections contribute bytes outside the media
			// segments counted below, so their presence prevents an exact total.
			allSegmentsRanged = false
			continue
		}
		if line == "#EXT-X-ENDLIST" {
			endList = true
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}

		if pendingVariant != nil {
			if variantURL, ok := resolveManifestURL(rawURL, line); ok {
				bandwidth := positiveInt64(pendingVariant["AVERAGE-BANDWIDTH"])
				if bandwidth == 0 {
					bandwidth = positiveInt64(pendingVariant["BANDWIDTH"])
				}
				if bandwidth > 0 {
					var width, height int
					fmt.Sscanf(pendingVariant["RESOLUTION"], "%dx%d", &width, &height)
					result.Variants = append(result.Variants, manifestVariant{URL: variantURL, Bandwidth: bandwidth, Width: width, Height: height, Codecs: cleanCodecs(pendingVariant["CODECS"])})
					if len(result.Variants) > maxManifestVariants {
						return manifestEstimate{}
					}
				}
			}
			pendingVariant = nil
			continue
		}

		segmentCount++
		if durationPending {
			if durationTotal > math.MaxFloat64-pendingDuration {
				durationValid = false
			} else {
				durationTotal += pendingDuration
			}
		} else {
			durationValid = false
		}
		if !rangePending || !rangeValid || result.Bytes > math.MaxInt64-pendingRange {
			allSegmentsRanged = false
		} else {
			result.Bytes += pendingRange
		}
		pendingDuration, durationPending = 0, false
		pendingRange, rangePending, rangeValid = 0, false, true
	}
	if scanner.Err() != nil || !headerSeen {
		return manifestEstimate{}
	}
	if durationPending {
		durationValid = false
	}
	if rangePending || !rangeValid {
		allSegmentsRanged = false
	}
	if master {
		return result
	}
	if !endList {
		return manifestEstimate{}
	}
	if segmentCount == 0 {
		return result
	}
	if durationValid {
		result.DurationSeconds = durationTotal
	}
	if allSegmentsRanged {
		result.Exact = true
	} else {
		result.Bytes = 0
	}
	return result
}

func parseHLSAttributes(raw string) map[string]string {
	attributes := make(map[string]string)
	for i := 0; i < len(raw); {
		for i < len(raw) && (raw[i] == ',' || raw[i] == ' ' || raw[i] == '\t') {
			i++
		}
		if i == len(raw) {
			break
		}
		keyStart := i
		for i < len(raw) && raw[i] != '=' && raw[i] != ',' {
			i++
		}
		if i == len(raw) || raw[i] != '=' {
			return nil
		}
		key := strings.ToUpper(strings.TrimSpace(raw[keyStart:i]))
		i++
		if key == "" {
			return nil
		}
		var value string
		if i < len(raw) && raw[i] == '"' {
			i++
			start := i
			for i < len(raw) && raw[i] != '"' {
				i++
			}
			if i == len(raw) {
				return nil
			}
			value = raw[start:i]
			i++
			if i < len(raw) && raw[i] != ',' {
				return nil
			}
		} else {
			start := i
			for i < len(raw) && raw[i] != ',' {
				i++
			}
			value = strings.TrimSpace(raw[start:i])
		}
		attributes[key] = value
		if i < len(raw) && raw[i] == ',' {
			i++
		}
	}
	return attributes
}

func parseHLSByteRange(raw string) (int64, bool) {
	lengthRaw, offsetRaw, hasOffset := strings.Cut(strings.TrimSpace(raw), "@")
	length := positiveInt64(lengthRaw)
	if length == 0 {
		return 0, false
	}
	if hasOffset {
		offset, err := strconv.ParseInt(strings.TrimSpace(offsetRaw), 10, 64)
		if err != nil || offset < 0 || offset > math.MaxInt64-length {
			return 0, false
		}
	}
	return length, true
}

type dashManifest struct {
	XMLName                   xml.Name     `xml:"MPD"`
	Type                      string       `xml:"type,attr"`
	MediaPresentationDuration string       `xml:"mediaPresentationDuration,attr"`
	Periods                   []dashPeriod `xml:"Period"`
}

type dashPeriod struct {
	Duration      string              `xml:"duration,attr"`
	AdaptationSet []dashAdaptationSet `xml:"AdaptationSet"`
}

type dashAdaptationSet struct {
	ContentType    string               `xml:"contentType,attr"`
	MIMEType       string               `xml:"mimeType,attr"`
	Codecs         string               `xml:"codecs,attr"`
	Representation []dashRepresentation `xml:"Representation"`
}

type dashRepresentation struct {
	Bandwidth   string `xml:"bandwidth,attr"`
	ContentType string `xml:"contentType,attr"`
	MIMEType    string `xml:"mimeType,attr"`
	Codecs      string `xml:"codecs,attr"`
	Width       int    `xml:"width,attr"`
	Height      int    `xml:"height,attr"`
}

func dashManifestSize(body string) manifestEstimate {
	var mpd dashManifest
	if err := xml.Unmarshal([]byte(body), &mpd); err != nil || mpd.XMLName.Local != "MPD" || strings.EqualFold(mpd.Type, "dynamic") {
		return manifestEstimate{}
	}
	duration, durationOK := parseISODuration(mpd.MediaPresentationDuration)
	if !durationOK && len(mpd.Periods) > 0 {
		duration = 0
		durationOK = true
		for _, period := range mpd.Periods {
			periodDuration, ok := parseISODuration(period.Duration)
			if !ok || duration > math.MaxFloat64-periodDuration {
				durationOK = false
				break
			}
			duration += periodDuration
		}
	}
	result := manifestEstimate{}
	if durationOK {
		result.DurationSeconds = duration
	}

	var maxVideo, maxAudio, maxCombined int64
	for _, period := range mpd.Periods {
		for _, adaptation := range period.AdaptationSet {
			for _, representation := range adaptation.Representation {
				bandwidth := positiveInt64(representation.Bandwidth)
				if bandwidth == 0 {
					continue
				}
				contentType := firstNonEmpty(representation.ContentType, adaptation.ContentType)
				mimeType := firstNonEmpty(representation.MIMEType, adaptation.MIMEType)
				codecs := firstNonEmpty(representation.Codecs, adaptation.Codecs)
				switch dashRepresentationKind(contentType, mimeType, codecs) {
				case "video":
					if representation.Height > result.Height {
						result.Width, result.Height, result.Codecs = representation.Width, representation.Height, cleanCodecs(codecs)
					}
					if bandwidth > maxVideo {
						maxVideo = bandwidth
					}
				case "audio":
					if bandwidth > maxAudio {
						maxAudio = bandwidth
					}
				case "combined":
					if bandwidth > maxCombined {
						maxCombined = bandwidth
					}
				}
			}
		}
	}
	bandwidth := maxCombined
	if maxVideo <= math.MaxInt64-maxAudio && maxVideo+maxAudio > bandwidth {
		bandwidth = maxVideo + maxAudio
	}
	if durationOK && duration > 0 && bandwidth > 0 {
		bytes := duration * float64(bandwidth) / 8
		if !math.IsInf(bytes, 0) && !math.IsNaN(bytes) && bytes > 0 && bytes < float64(math.MaxInt64) {
			result.Bytes = int64(math.Round(bytes))
		}
	}
	return result
}

func dashRepresentationKind(contentType, mimeType, codecs string) string {
	videoCodec, audioCodec := false, false
	for _, codec := range strings.Split(strings.ToLower(codecs), ",") {
		codec = strings.TrimSpace(codec)
		if hasPrefix(codec, "avc1", "avc3", "hev1", "hvc1", "vp08", "vp09", "av01", "theora", "mp4v") {
			videoCodec = true
		}
		if hasPrefix(codec, "mp4a", "aac", "ac-3", "ec-3", "opus", "vorbis", "flac", "alac") {
			audioCodec = true
		}
	}
	if videoCodec && audioCodec {
		return "combined"
	}
	if videoCodec {
		return "video"
	}
	if audioCodec {
		return "audio"
	}
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "video":
		return "video"
	case "audio":
		return "audio"
	case "text", "image", "application":
		return ""
	}
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mimeType, "video/") {
		return "video"
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(mimeType, "text/") || strings.HasPrefix(mimeType, "application/") || strings.HasPrefix(mimeType, "image/") {
		return ""
	}
	return "combined"
}

func parseISODuration(raw string) (float64, bool) {
	parts := isoDurationPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if parts == nil || (parts[1] == "" && parts[2] == "" && parts[3] == "" && parts[4] == "") {
		return 0, false
	}
	units := []float64{86400, 3600, 60, 1}
	total := float64(0)
	for i, part := range parts[1:] {
		if part == "" {
			continue
		}
		value, err := strconv.ParseFloat(part, 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || value < 0 {
			return 0, false
		}
		add := value * units[i]
		if math.IsInf(add, 0) || total > math.MaxFloat64-add {
			return 0, false
		}
		total += add
	}
	return total, true
}

func positiveInt64(raw string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func parsePositiveOrZeroFloat(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	return value, true
}

func resolveManifestURL(baseRaw, reference string) (string, bool) {
	base, err := url.Parse(baseRaw)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(ref)
	if (resolved.Scheme != "http" && resolved.Scheme != "https") || resolved.Hostname() == "" || resolved.User != nil {
		return "", false
	}
	resolved.Fragment = ""
	return resolved.String(), true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func hasPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
