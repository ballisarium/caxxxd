package browser

import "testing"

func TestManifestSizeHLSExactBytesRequireCompletePayload(t *testing.T) {
	got := manifestSize("https://media.example/master/track.m3u8", "HLS", `#EXTM3U
#EXTINF:5.0,
#EXT-X-BYTERANGE:100@0
segment.bin
#EXTINF:7.5,
#EXT-X-BYTERANGE:120@100
segment.bin
#EXT-X-ENDLIST
`)
	if got.DurationSeconds != 12.5 || got.Bytes != 220 || !got.Exact {
		t.Fatalf("manifest estimate = %+v, want 12.5 seconds and exactly 220 bytes", got)
	}

	partialRange := manifestSize("https://media.example/master/track.m3u8", "HLS", `#EXTM3U
#EXTINF:5,
#EXT-X-BYTERANGE:100@0
segment.bin
#EXTINF:7,
other.bin
#EXT-X-ENDLIST
`)
	if partialRange.DurationSeconds != 12 || partialRange.Bytes != 0 || partialRange.Exact {
		t.Fatalf("partly bounded HLS estimate = %+v, want duration only", partialRange)
	}

	initializationMap := manifestSize("https://media.example/master/track.m3u8", "HLS", `#EXTM3U
#EXT-X-MAP:URI="init.mp4"
#EXTINF:5,
#EXT-X-BYTERANGE:100@0
segment.mp4
#EXT-X-ENDLIST
`)
	if initializationMap.DurationSeconds != 5 || initializationMap.Bytes != 0 || initializationMap.Exact {
		t.Fatalf("HLS estimate with initialization map = %+v, want duration only", initializationMap)
	}
}

func TestManifestSizeHLSLivePlaylistHasNoTotal(t *testing.T) {
	got := manifestSize("https://media.example/live/index.m3u8", "HLS", `#EXTM3U
#EXTINF:5,
#EXT-X-BYTERANGE:100@0
segment.bin
`)
	if got.DurationSeconds != 0 || got.Bytes != 0 || got.Exact {
		t.Fatalf("live playlist estimate = %+v, want no claimed total", got)
	}
}

func TestManifestSizeHLSMasterResolvesVariantsAndUsesAverageBandwidth(t *testing.T) {
	got := manifestSize("https://media.example/watch/master.m3u8", "HLS", `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=2800000,AVERAGE-BANDWIDTH=2100000,CODECS="avc1.4d401f,mp4a.40.2"
video/high.m3u8?quality=high
#EXT-X-STREAM-INF:BANDWIDTH=900000
https://cdn.example/low.m3u8
`)
	if len(got.Variants) != 2 {
		t.Fatalf("variants = %+v, want two variants", got.Variants)
	}
	want := []manifestVariant{
		{URL: "https://media.example/watch/video/high.m3u8?quality=high", Bandwidth: 2100000, Codecs: "avc1.4d401f,mp4a.40.2"},
		{URL: "https://cdn.example/low.m3u8", Bandwidth: 900000},
	}
	for i := range want {
		if got.Variants[i] != want[i] {
			t.Fatalf("variant %d = %+v, want %+v", i, got.Variants[i], want[i])
		}
	}
}

func TestManifestSizeDASHAddsLargestSeparateAudioAndVideoRates(t *testing.T) {
	got := manifestSize("https://media.example/manifest.mpd", "DASH", `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT1H2M3.5S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <Representation bandwidth="2500000"/>
      <Representation bandwidth="3500000"/>
    </AdaptationSet>
    <AdaptationSet contentType="audio" mimeType="audio/mp4">
      <Representation bandwidth="192000"/>
      <Representation bandwidth="128000"/>
    </AdaptationSet>
  </Period>
</MPD>`)
	wantDuration := 3723.5
	wantBytes := int64(wantDuration * 3692000 / 8)
	if got.DurationSeconds != wantDuration || got.Bytes != wantBytes || got.Exact {
		t.Fatalf("DASH estimate = %+v, want %v seconds, about %d bytes, approximate", got, wantDuration, wantBytes)
	}

	combined := manifestSize("https://media.example/combined.mpd", "DASH", `<MPD mediaPresentationDuration="PT10S"><Period>
  <AdaptationSet contentType="video"><Representation bandwidth="5000000" codecs="avc1.4d401f,mp4a.40.2"/></AdaptationSet>
  <AdaptationSet contentType="audio"><Representation bandwidth="128000"/></AdaptationSet>
</Period></MPD>`)
	if combined.Bytes != 6250000 || combined.Exact {
		t.Fatalf("combined DASH estimate = %+v, want largest combined rate, about 6250000 bytes", combined)
	}
}

func TestManifestSizeDASHEstimatesSingleTrackBandwidth(t *testing.T) {
	for _, track := range []struct {
		name, contentType, bandwidth string
		wantBytes                    int64
	}{
		{name: "video", contentType: "video", bandwidth: "2500000", wantBytes: 3125000},
		{name: "audio", contentType: "audio", bandwidth: "192000", wantBytes: 240000},
	} {
		t.Run(track.name, func(t *testing.T) {
			body := `<MPD mediaPresentationDuration="PT10S"><Period><AdaptationSet contentType="` + track.contentType + `"><Representation bandwidth="` + track.bandwidth + `"/></AdaptationSet></Period></MPD>`
			got := manifestSize("https://media.example/manifest.mpd", "DASH", body)
			if got.DurationSeconds != 10 || got.Bytes != track.wantBytes || got.Exact {
				t.Fatalf("single-track DASH estimate = %+v, want %d bytes, approximate", got, track.wantBytes)
			}
		})
	}
}

func TestManifestSizeDASHPeriodDurationAndDynamicManifest(t *testing.T) {
	period := manifestSize("https://media.example/period.mpd", "DASH", `<MPD><Period duration="PT12.5S"/></MPD>`)
	if period.DurationSeconds != 12.5 {
		t.Fatalf("period duration = %+v, want 12.5 seconds", period)
	}
	dynamic := manifestSize("https://media.example/live.mpd", "DASH", `<MPD type="dynamic" mediaPresentationDuration="PT12.5S"><Period><AdaptationSet contentType="video"><Representation bandwidth="1000000"/></AdaptationSet></Period></MPD>`)
	if dynamic.DurationSeconds != 0 || dynamic.Bytes != 0 || dynamic.Exact {
		t.Fatalf("dynamic manifest estimate = %+v, want no claimed total", dynamic)
	}
}

func TestManifestSizeRejectsMalformedInputs(t *testing.T) {
	for _, input := range []struct {
		name, rawURL, kind, body string
	}{
		{name: "unsafe base URL", rawURL: "javascript:alert(1)", kind: "HLS", body: "#EXTM3U\n#EXT-X-ENDLIST"},
		{name: "unknown kind", rawURL: "https://media.example/manifest", kind: "OTHER", body: "#EXTM3U\n#EXT-X-ENDLIST"},
		{name: "malformed DASH", rawURL: "https://media.example/manifest.mpd", kind: "DASH", body: `<MPD><Period>`},
	} {
		t.Run(input.name, func(t *testing.T) {
			got := manifestSize(input.rawURL, input.kind, input.body)
			if got.DurationSeconds != 0 || got.Bytes != 0 || got.Exact || len(got.Variants) != 0 {
				t.Fatalf("estimate = %+v, want empty result", got)
			}
		})
	}
}
