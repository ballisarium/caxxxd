package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ballisarium/caxxxd/internal/config"
	"github.com/ballisarium/caxxxd/internal/domain"
	"github.com/ballisarium/caxxxd/internal/ui"
	"github.com/ballisarium/caxxxd/internal/ytdlp"
)

// stage is one place the conversation can be. Unlike a screen, a stage is only
// ever entered by finishing the one before it or by going back to it.
type stage int

const (
	stageLink stage = iota
	stageRange
	stageMode
	stageVideoQuality
	stageVideoContainer
	stageAudioFormat
	stageManualVideo
	stageManualAudio
	stageReview
	stageDownload
)

// stepTitles are the steps the status bar counts. Several stages share one
// step: choosing a container is still choosing a format.
var stepTitles = []string{"Link", "Media", "Format", "Review", "Download"}

// stepOf maps a stage onto its step number and title.
func stepOf(current stage) (int, string) {
	switch current {
	case stageLink:
		return 1, stepTitles[0]
	case stageRange, stageMode:
		return 2, stepTitles[1]
	case stageReview:
		return 4, stepTitles[3]
	case stageDownload:
		return 5, stepTitles[4]
	default:
		return 3, stepTitles[2]
	}
}

// screen starts the display for a stage: a cleared terminal, the status bar,
// whatever the previous screen left to say, and the media being decided about.
//
// Everything a step draws is drawn again here, every time, which is what makes
// a step a screen rather than another entry in a growing transcript.
func (a *App) screen(current stage) {
	a.current = current
	number, title := stepOf(current)

	a.console.Screen()
	if current == stageLink {
		// The link step is where a session starts, and the only screen with
		// room for the logo: everywhere else that space belongs to the media.
		a.console.Logo(a.options.Version)
	}
	a.console.Step(number, title)
	a.deliver()

	if a.info.Title != "" && showsMedia(current) {
		a.showMedia()
	}
}

// showsMedia reports whether a stage is a decision about the media itself and
// should keep it in view. The review draws its own summary, and the link step
// is where a new item is chosen.
func showsMedia(current stage) bool {
	switch current {
	case stageRange, stageMode, stageVideoQuality, stageVideoContainer,
		stageAudioFormat, stageManualVideo, stageManualAudio:
		return true
	default:
		return false
	}
}

// step runs one stage and reports which stage comes next.
func (a *App) step(ctx context.Context, current stage) (stage, error) {
	a.screen(current)

	switch current {
	case stageLink:
		return a.askLink(ctx)
	case stageRange:
		return a.askRange()
	case stageMode:
		return a.askMode()
	case stageVideoQuality:
		return a.askVideoQuality()
	case stageVideoContainer:
		return a.askContainer()
	case stageAudioFormat:
		return a.askAudioFormat()
	case stageManualVideo:
		return a.askManualVideo()
	case stageManualAudio:
		return a.askManualAudio()
	case stageReview:
		return a.askReview()
	case stageDownload:
		return a.runDownload(ctx)
	default:
		return stageLink, nil
	}
}

// askLink takes a URL and turns it into media the rest of the flow can talk
// about. It is also where a failed lookup comes back to.
func (a *App) askLink(ctx context.Context) (stage, error) {
	// A URL from the command line is offered as a prefilled answer, and so is
	// the one already loaded: coming back to this step to fix a typo should
	// not mean typing the whole link again.
	prefill := a.initialURL
	if prefill == "" {
		prefill = a.url
	}

	typed, err := a.prompt.Text(
		"Paste a media URL",
		"One item per run. Playlists are not supported yet.",
		prefill,
	)
	if err != nil {
		return stageLink, err
	}
	a.initialURL = ""

	value := strings.TrimSpace(typed)
	if value == "" {
		a.carry("Nothing to download", "A media URL is what caxxxd starts from.")
		return stageLink, nil
	}

	a.url = value
	if !a.sectionFixed {
		a.options.Section = nil
	}
	return a.fetchMetadata(ctx)
}

// fetchMetadata asks yt-dlp what the link actually is.
func (a *App) fetchMetadata(ctx context.Context) (stage, error) {
	// The link is on the line right above this one; repeating it here would
	// only push the title that comes next further down.
	spinner := a.console.Spinner("Asking yt-dlp what this is")
	defer spinner.Stop()

	var (
		info ytdlp.MediaInfo
		err  error
	)
	if interrupted(ctx, func(runCtx context.Context) {
		info, err = a.options.Client.Fetch(runCtx, a.url)
	}) {
		return stageLink, errQuit
	}

	if err != nil {
		spinner.Fail("That link did not resolve")
		// The rejected URL is not a loaded item. Do not offer it as the next
		// answer when the user chooses to try another link.
		a.url = ""
		return a.reportFailure(classifyMetadataError(err),
			recovery{"Try another link", "start again from the URL", stageLink},
		)
	}

	if a.options.Section != nil {
		if err := a.options.Section.ValidateDuration(info.Duration); err != nil {
			spinner.Fail("That time range is not valid")
			return stageLink, fmt.Errorf("invalid --section %q: %w", a.options.Section.String(), err)
		}
	}

	spinner.Done("Found " + ui.Truncate(info.Title, a.console.Width()/2))
	a.info = info
	a.clearManualSelection()
	if !a.sectionFixed {
		return stageRange, nil
	}
	return stageMode, nil
}

// showMedia is the card every later step refers back to.
func (a *App) showMedia() {
	platform := a.info.Platform
	if platform == "Youtube" {
		platform = "YouTube"
	}

	fields := []ui.Field{}
	if a.info.Uploader != "" {
		fields = append(fields, ui.Field{Label: "Uploader", Value: a.info.Uploader})
	}
	if platform != "" {
		fields = append(fields, ui.Field{Label: "Platform", Value: platform})
	}
	fields = append(fields,
		ui.Field{Label: "Duration", Value: ui.FormatDuration(a.info.Duration)},
		ui.Field{Label: "Streams", Value: streamSummary(a.info.Formats)},
	)

	a.console.Panel(ui.Truncate(a.info.Title, a.console.Width()-8), a.console.Fields(fields)...)
}

// streamSummary counts what the site is actually offering. The two halves
// overlap by design: a combined stream carries both picture and sound.
func streamSummary(formats []ytdlp.Format) string {
	video := len(ytdlp.FormatsByKind(formats, ytdlp.FormatKindMuxed, ytdlp.FormatKindVideo))
	audio := len(ytdlp.FormatsByKind(formats, ytdlp.FormatKindAudio))

	return strconv.Itoa(len(formats)) + " downloadable  ·  " +
		strconv.Itoa(video) + " with picture  ·  " +
		strconv.Itoa(audio) + " audio only"
}

// askMode is the top-level choice: keep the picture, or only the sound.
func (a *App) askMode() (stage, error) {
	choices := []Choice{
		{Label: "Video", Detail: "picture and sound, merged into one file"},
		{Label: "Audio", Detail: "sound only, ready for a music library"},
		backChoice,
	}

	picked, err := a.prompt.Choose("What do you want out of it?", choices, 0)
	if err != nil {
		return stageMode, err
	}

	switch picked {
	case 0:
		a.mode = domain.MediaModeVideo
		a.clearManualSelection()
		return stageVideoQuality, nil
	case 1:
		a.mode = domain.MediaModeAudio
		a.clearManualSelection()
		return stageAudioFormat, nil
	default:
		return stageRange, nil
	}
}

// askRange decides whether this item should be downloaded whole or clipped.
// CLI-provided ranges never enter this stage: they are an explicit choice that
// has already been parsed before the interface starts.
func (a *App) askRange() (stage, error) {
	if a.rangeRetry {
		a.rangeRetry = false
		return a.askRangeInput()
	}

	duration := ui.FormatDuration(a.info.Duration)
	choices := []Choice{
		{Label: "Whole video", Detail: "download all " + duration},
		{Label: "Choose a range", Detail: "download only part of this video"},
		backChoice,
	}

	picked, err := a.prompt.Choose("Time range", choices, 0)
	if err != nil {
		return stageRange, err
	}

	switch picked {
	case 0:
		a.options.Section = nil
		return a.finishRange(), nil
	case 1:
		return a.askRangeInput()
	default:
		return a.backFromRange(), nil
	}
}

func (a *App) askRangeInput() (stage, error) {
	duration := ui.FormatDuration(a.info.Duration)
	initial := ""
	if a.options.Section != nil {
		initial = a.options.Section.String()
	}
	typed, err := a.prompt.Text(
		"Time range",
		"Use SS, MM:SS, or HH:MM:SS. Video duration: "+duration+".",
		initial,
	)
	if err != nil {
		return stageRange, err
	}

	section, parseErr := domain.ParseTimeRange(strings.TrimSpace(typed))
	if parseErr != nil {
		return a.rangeError(parseErr), nil
	}
	if durationErr := section.ValidateDuration(a.info.Duration); durationErr != nil {
		return a.rangeError(durationErr), nil
	}

	a.options.Section = &section
	return a.finishRange(), nil
}

func (a *App) rangeError(err error) stage {
	a.rangeRetry = true
	a.carry(
		"That time range is not valid",
		err.Error(),
		"Use START-END, for example 00:30-01:20.",
	)
	return stageRange
}

func (a *App) finishRange() stage {
	a.rangeRetry = false
	next := a.rangeReturn
	a.rangeReturn = stageMode
	return next
}

func (a *App) backFromRange() stage {
	a.rangeRetry = false
	next := a.rangeReturn
	a.rangeReturn = stageMode
	if next == stageReview {
		return stageReview
	}
	return stageLink
}

// askVideoQuality offers the presets, plus the exact stream table.
func (a *App) askVideoQuality() (stage, error) {
	presets := domain.VideoPresets()
	choices := make([]Choice, 0, len(presets)+1)
	for _, preset := range presets {
		choices = append(choices, Choice{Label: preset.Label, Detail: qualityDetail(preset)})
	}
	choices = append(choices, backChoice)

	a.console.Hint("caxxxd never transcodes video, so quality is only ever the source quality.")
	picked, err := a.prompt.Choose("Video quality", choices, 0)
	if err != nil {
		return stageVideoQuality, err
	}
	if picked == len(choices)-1 {
		return stageMode, nil
	}

	preset := presets[picked]
	if preset.Manual {
		if len(a.videoStreams()) == 0 {
			a.carry("No video streams", "This item has nothing with a picture to choose from.")
			return stageVideoQuality, nil
		}
		return stageManualVideo, nil
	}

	a.maxHeight = preset.MaxHeight
	a.clearManualSelection()
	return stageVideoContainer, nil
}

func qualityDetail(preset domain.VideoPreset) string {
	switch {
	case preset.Manual:
		return "pick exact streams from the table"
	case preset.MaxHeight == 0:
		return "whatever the site offers"
	default:
		return "never taller than this"
	}
}

// containerChoices are the containers caxxxd can remux into, in display order.
var containerChoices = []struct {
	container domain.VideoContainer
	label     string
	detail    string
	note      string
}{
	{
		container: domain.VideoContainerMKV,
		label:     "MKV",
		detail:    "maximum source quality, recommended",
	},
	{
		container: domain.VideoContainerMP4,
		label:     "MP4",
		detail:    "H.264 + AAC/M4A where available, better Apple compatibility",
		note:      "MP4 compatibility may limit the maximum resolution, because caxxxd does not transcode video.",
	},
	{
		container: domain.VideoContainerWebM,
		label:     "WebM",
		detail:    "VP9/AV1 + Opus where available",
	},
}

func containerIndex(container domain.VideoContainer) int {
	for index, option := range containerChoices {
		if option.container == container {
			return index
		}
	}
	return 0
}

// askContainer picks the file the streams are merged into.
func (a *App) askContainer() (stage, error) {
	choices := make([]Choice, 0, len(containerChoices)+1)
	for _, option := range containerChoices {
		choices = append(choices, Choice{Label: option.label, Detail: option.detail})
	}
	choices = append(choices, backChoice)

	picked, err := a.prompt.Choose("Container", choices, containerIndex(a.container))
	if err != nil {
		return stageVideoContainer, err
	}
	if picked == len(choices)-1 {
		return stageVideoQuality, nil
	}

	option := containerChoices[picked]
	a.container = option.container
	if option.note != "" {
		a.carry("About "+option.label, option.note)
	}
	return stageReview, nil
}

func audioPresetIndex(format domain.AudioFormat) int {
	for index, preset := range domain.AudioPresets() {
		if preset.Format == format && !preset.Manual {
			return index
		}
	}
	return 0
}

// audioPresetDetails explains each audio choice in one honest line.
var audioPresetDetails = map[string]string{
	"source": "avoids unnecessary re-encoding",
	"opus":   "efficient and high quality",
	"m4a":    "strong Apple compatibility",
	"mp3":    "widest compatibility, re-encoded",
	"flac":   "lossless container, but the source is already lossy",
	"wav":    "uncompressed output, but the source is already lossy",
	"manual": "pick an exact audio stream yourself",
}

// askAudioFormat picks what the sound is written as.
func (a *App) askAudioFormat() (stage, error) {
	presets := domain.AudioPresets()
	choices := make([]Choice, 0, len(presets)+1)
	for _, preset := range presets {
		choices = append(choices, Choice{Label: preset.Label, Detail: audioPresetDetails[preset.ID]})
	}
	choices = append(choices, backChoice)

	picked, err := a.prompt.Choose("Audio format", choices, audioPresetIndex(a.audioFormat))
	if err != nil {
		return stageAudioFormat, err
	}
	if picked == len(choices)-1 {
		return stageMode, nil
	}

	preset := presets[picked]
	if preset.Manual {
		if len(a.audioStreams()) == 0 {
			a.carry("No separate audio streams", "This item only offers sound merged with picture.")
			return stageAudioFormat, nil
		}
		return stageManualAudio, nil
	}

	if preset.ShowLossySourceWarning {
		accepted, err := a.confirmLossyConversion(preset.Format)
		if err != nil {
			return stageAudioFormat, err
		}
		if !accepted {
			return stageAudioFormat, nil
		}
	}

	a.audioFormat = preset.Format
	a.manual.audioID = ""
	return stageReview, nil
}

// confirmLossyConversion says plainly that the conversion cannot help, then
// lets the user have it anyway.
func (a *App) confirmLossyConversion(format domain.AudioFormat) (bool, error) {
	name := strings.ToUpper(string(format))
	a.console.Notice("Converting to "+name+" cannot recover quality",
		"The source audio is already lossy. Converting it to "+name+" makes a much larger",
		"file without bringing back any of the detail the site threw away.",
		"",
		a.console.Theme().Slate.Sprint("\"Best source audio\" is the highest fidelity actually available."),
	)

	picked, err := a.prompt.Choose("Continue with "+name+"?", []Choice{
		{Label: "Yes, use " + name, Detail: "I know what it costs"},
		{Label: "No, choose another format", Detail: "go back to the list"},
	}, 1)
	return picked == 0, err
}

// askManualVideo is the exact stream table for anything carrying a picture.
func (a *App) askManualVideo() (stage, error) {
	streams := a.videoStreams()
	table := tableFor(a.console.Width())
	choices := streamChoices(streams, table)
	choices = append(choices, backChoice)

	if filterable(len(choices)) {
		a.console.Hint("Type to search the table: an id, a resolution, a codec, or a container.")
	}
	a.console.Accent(table.header())
	picked, err := a.prompt.Choose("Choose a video stream", choices, 0)
	if err != nil {
		return stageManualVideo, err
	}
	if picked == len(choices)-1 {
		return stageVideoQuality, nil
	}

	selected := streams[picked]
	if selected.Kind == ytdlp.FormatKindMuxed {
		a.manual = manualSelection{muxedID: selected.ID}
		return stageReview, nil
	}

	// A video-only stream is only half a download; it needs sound next. When
	// there is no sound to be had, saying so beats sending someone into an
	// empty table whose only row is the way back out of it.
	if len(a.audioStreams()) == 0 {
		a.carry("Nothing to pair "+selected.ID+" with",
			"This item offers no separate audio stream, so a video-only one cannot be completed.",
			"Choose a stream that already carries sound.")
		return stageManualVideo, nil
	}

	a.manual.muxedID = ""
	a.manual.videoID = selected.ID
	return stageManualAudio, nil
}

// askManualAudio is the exact stream table for sound.
func (a *App) askManualAudio() (stage, error) {
	streams := a.audioStreams()
	table := tableFor(a.console.Width())
	choices := streamChoices(streams, table)
	choices = append(choices, backChoice)

	question := "Choose an audio stream"
	if a.manual.videoID != "" {
		question += " to pair with " + a.manual.videoID
	}

	if filterable(len(choices)) {
		a.console.Hint("Type to search the table: an id, a bitrate, a codec, or a container.")
	}
	a.console.Accent(table.header())
	picked, err := a.prompt.Choose(question, choices, 0)
	if err != nil {
		return stageManualAudio, err
	}
	if picked == len(choices)-1 {
		// Half a pair is not a selection: drop it on the way back.
		if a.mode == domain.MediaModeVideo {
			a.manual.videoID = ""
			return stageManualVideo, nil
		}
		return stageAudioFormat, nil
	}

	a.manual.audioID = streams[picked].ID
	if a.mode == domain.MediaModeAudio {
		a.audioFormat = domain.AudioFormatSource
	}
	return stageReview, nil
}

func (a *App) videoStreams() []ytdlp.Format {
	return ytdlp.FormatsByKind(a.info.Formats, ytdlp.FormatKindMuxed, ytdlp.FormatKindVideo)
}

func (a *App) audioStreams() []ytdlp.Format {
	return ytdlp.FormatsByKind(a.info.Formats, ytdlp.FormatKindAudio)
}

// askReview is the last look before anything is written to disk.
func (a *App) askReview() (stage, error) {
	a.console.Panel("Ready to download", a.console.Fields(a.reviewFields())...)

	choices := []Choice{
		{Label: "Download", Detail: "run yt-dlp with exactly this"},
		{Label: "Change download folder", Detail: collapseHome(a.outputDir, a.options.Home)},
	}
	changeRange := -1
	if !a.sectionFixed {
		changeRange = len(choices)
		choices = append(choices, Choice{Label: "Change time range", Detail: "choose a different part of the video"})
	}
	backIndex := len(choices)
	choices = append(choices, backChoice)
	quitIndex := len(choices)
	choices = append(choices, quitChoice)

	picked, err := a.prompt.Choose("Start?", choices, 0)
	if err != nil {
		return stageReview, err
	}

	switch picked {
	case 0:
		return stageDownload, nil
	case 1:
		if err := a.askDestination(); err != nil {
			return stageReview, err
		}
		return stageReview, nil
	case changeRange:
		a.rangeReturn = stageReview
		return stageRange, nil
	case backIndex:
		return a.reviewReturn(), nil
	case quitIndex:
		return stageReview, errQuit
	default:
		return stageReview, errQuit
	}
}

// reviewFields is the plan, in the order someone checking it would read it.
func (a *App) reviewFields() []ui.Field {
	fields := []ui.Field{{Label: "Title", Value: ui.Truncate(a.info.Title, a.console.Width()-20)}}
	if a.options.Section != nil {
		fields = append(fields, ui.Field{Label: "Time range", Value: a.options.Section.String()})
	}

	if a.mode == domain.MediaModeVideo {
		fields = append(fields, ui.Field{Label: "Mode", Value: "Video"})
		switch {
		case a.manual.muxedID != "":
			fields = append(fields, ui.Field{Label: "Stream", Value: a.manual.muxedID})
		case a.manual.videoID != "":
			fields = append(fields, ui.Field{Label: "Streams", Value: a.manual.videoID + " + " + a.manual.audioID})
		default:
			fields = append(fields,
				ui.Field{Label: "Quality", Value: qualityLabel(a.maxHeight)},
				ui.Field{Label: "Container", Value: strings.ToUpper(string(a.container))},
			)
		}
	} else {
		fields = append(fields, ui.Field{Label: "Mode", Value: "Audio"})
		if a.manual.audioID != "" {
			fields = append(fields, ui.Field{Label: "Stream", Value: a.manual.audioID})
		}
		fields = append(fields, ui.Field{Label: "Format", Value: audioFormatLabel(a.audioFormat)})
	}

	return append(fields, ui.Field{Label: "Destination", Value: collapseHome(a.outputDir, a.options.Home)})
}

// reviewReturn is the last stage that actually changed the selection.
func (a *App) reviewReturn() stage {
	if a.mode == domain.MediaModeAudio {
		if a.manual.audioID != "" {
			return stageManualAudio
		}
		return stageAudioFormat
	}
	if a.manual.chosen() {
		return stageManualVideo
	}
	return stageVideoContainer
}

func qualityLabel(maxHeight int) string {
	if maxHeight <= 0 {
		return "Best available"
	}
	return "Up to " + strconv.Itoa(maxHeight) + "p"
}

func audioFormatLabel(format domain.AudioFormat) string {
	if format == domain.AudioFormatSource || format == "" {
		return "Best source audio"
	}
	return strings.ToUpper(string(format))
}

// askDestination edits the download folder. The directory itself is only
// created when the download starts.
func (a *App) askDestination() error {
	typed, err := a.prompt.Text(
		"Download folder",
		"A full path, for example ~/Movies.",
		collapseHome(a.outputDir, a.options.Home),
	)
	if err != nil {
		return err
	}

	expanded := config.ExpandHome(strings.TrimSpace(typed), a.options.Home)
	if expanded == "" || !filepath.IsAbs(expanded) {
		a.carry("That is not a folder caxxxd can use",
			"Enter a full path, for example ~/Movies. The folder is created when the download starts.")
		return nil
	}

	a.outputDir = filepath.Clean(expanded)
	a.console.SetDestination(collapseHome(a.outputDir, a.options.Home))
	return nil
}
