package browser

import (
	"context"
	"time"
)

// Read playback properties from captured players, never unrelated user tabs.
func (s *Session) startMetadataWatch() {
	s.metadataOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				select {
				case <-s.conn.done:
					cancel()
				case <-ctx.Done():
				}
			}()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				s.mu.Lock()
				players := make(map[string]bool)
				for _, item := range s.items {
					if item.session != "" {
						players[item.session] = true
					}
				}
				s.mu.Unlock()
				for session := range players {
					run, cancelRead := context.WithTimeout(ctx, time.Second)
					var result struct {
						Result struct {
							Value []struct {
								URL      string  `json:"url"`
								Duration float64 `json:"duration"`
								Width    int     `json:"width"`
								Height   int     `json:"height"`
							} `json:"value"`
						} `json:"result"`
					}
					err := s.conn.call(run, session, "Runtime.evaluate", map[string]any{"returnByValue": true, "expression": `(()=>{const out=[];const visit=d=>{if(!d)return;for(const v of d.querySelectorAll('video,audio'))out.push({url:v.currentSrc,duration:Number.isFinite(v.duration)?v.duration:0,width:v.videoWidth||0,height:v.videoHeight||0});for(const f of d.querySelectorAll('iframe,frame')){try{visit(f.contentDocument)}catch{}}};visit(document);return out})()`}, &result)
					cancelRead()
					if err != nil {
						continue
					}
					s.mu.Lock()
					for _, media := range result.Result.Value {
						for i := range s.items {
							item := &s.items[i]
							if item.url != media.URL || item.session != session {
								continue
							}
							if media.Duration > 0 {
								item.Duration = media.Duration
							}
							if media.Height > 0 {
								item.Width, item.Height = media.Width, media.Height
							}
						}
					}
					s.mu.Unlock()
				}
			}
		}()
	})
}
