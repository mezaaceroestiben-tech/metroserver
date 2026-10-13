package server

// Extras de miliMusic para el servidor de "Escuchar juntos".
// Generado por Mega panel de GitHub. Agrega, sin tocar el protocolo original:
//   1) Mensajes de voz: el tipo "chat" reenvía a los demás de la sala los bytes
//      que llegan (la app los cifra de extremo a extremo; el servidor no los lee).
//   2) Página de invitación /listen?code=XXXX con el nombre de la app, y
//      /.well-known/assetlinks.json para que Android abra la app directo.
//   3) Modos de propuesta de canción: ahora, después de la actual o al final.

import (
	"encoding/json"
	"html"
	"net/http"
	"os"
	"strings"

	pb "github.com/MetrolistGroup/metroserver/proto"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

const (
	miliMsgTypeChat = "chat"
	// Tope de un mensaje de voz (cifrado). El límite de lectura del servidor es 512 KB.
	miliMaxChatBytes = 400 * 1024

	miliModeNow   = "now"
	miliModeNext  = "next"
	miliModeQueue = "queue"
)

// ---------- 1) Mensajes de voz ----------

func (s *Server) handleChat(c *Client, payload []byte) {
	if len(payload) == 0 {
		return
	}
	if len(payload) > miliMaxChatBytes {
		c.sendError(s.logger, "chat_too_large", "Voice message is too large")
		return
	}
	room := c.currentRoom()
	if room == nil {
		c.sendError(s.logger, "not_in_room", "You are not in a room")
		return
	}
	senderID := c.clientID()

	room.mu.RLock()
	if room.Clients[senderID] != c {
		room.mu.RUnlock()
		c.sendError(s.logger, "not_in_room", "You are not in a room")
		return
	}
	recipients := make([]*Client, 0, len(room.Clients))
	for id, other := range room.Clients {
		if id != senderID && other != nil {
			recipients = append(recipients, other)
		}
	}
	room.mu.RUnlock()

	if len(recipients) == 0 {
		return
	}
	// Se reenvía tal cual, sin comprimir: ya viene cifrado.
	data, err := proto.Marshal(&pb.Envelope{Type: miliMsgTypeChat, Payload: payload, Compressed: false})
	if err != nil {
		s.logger.Error("Error encoding chat", zap.Error(err))
		return
	}
	for _, other := range recipients {
		other.sendEncodedMessage(s.logger, miliMsgTypeChat, data)
	}
	s.logger.Info("Voice message relayed",
		zap.String("room_code", room.Code),
		zap.Int("bytes", len(payload)),
		zap.Int("recipients", len(recipients)))
}

// ---------- 3) Modos de propuesta ----------

// La app avisa qué quiere hacer con la canción en el campo suggested_by ("mm:now", "mm:next", "mm:queue").
func miliModeOf(tag string) string {
	switch strings.TrimSpace(tag) {
	case "mm:now":
		return miliModeNow
	case "mm:queue":
		return miliModeQueue
	}
	return miliModeNext
}

func miliModeFor(sug *Suggestion) string {
	if sug == nil || sug.Track == nil {
		return miliModeNext
	}
	return miliModeOf(sug.Track.SuggestedBy)
}

// Pone la canción aprobada en la cola según el modo.
func miliPlace(queue []TrackInfo, track TrackInfo, mode string) []TrackInfo {
	if mode == miliModeQueue {
		return append(append([]TrackInfo(nil), queue...), track)
	}
	return append([]TrackInfo{track}, queue...)
}

// ---------- 2) Página de invitación y enlaces de app ----------

type miliConfig struct {
	AppName string   `json:"appName"`
	Package string   `json:"package"`
	Sha256  []string `json:"sha256"`
}

func miliLoadConfig() miliConfig {
	cfg := miliConfig{AppName: "miliMusic", Package: "com.estiben.milimusic"}
	path := os.Getenv("MILI_CONFIG_FILE")
	if path == "" {
		path = "/app/mili_config.json"
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	if strings.TrimSpace(cfg.AppName) == "" {
		cfg.AppName = "miliMusic"
	}
	return cfg
}

func miliCleanCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() >= 12 {
			break
		}
	}
	return b.String()
}

const miliLandingHTML = `<!doctype html>
<html lang="es"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{APP}} · Escuchar juntos</title>
<meta property="og:type" content="website">
<meta property="og:site_name" content="{{APP}}">
<meta property="og:title" content="{{APP}} · Escuchar juntos">
<meta property="og:description" content="{{DESC}}">
<meta name="description" content="{{DESC}}">
<style>
body{margin:0;font-family:system-ui,sans-serif;background:#121218;color:#fff;display:flex;min-height:100vh;align-items:center;justify-content:center;text-align:center}
main{padding:24px;max-width:420px}
h1{font-size:1.6rem;margin:0 0 8px}
.code{font-size:2.4rem;letter-spacing:.25em;font-weight:700;margin:18px 0;color:#7CFFB2}
a.btn{display:block;background:#7CFFB2;color:#0b0b10;text-decoration:none;font-weight:700;padding:16px;border-radius:14px;margin:18px 0}
p{color:#c8c8d4;line-height:1.5}
</style></head><body><main>
<h1>{{APP}}</h1>
<p>{{DESC}}</p>
{{CODEBLOCK}}
<a class="btn" id="abrir" href="{{INTENT}}">Abrir en {{APP}}</a>
<p>Si la app no se abre sola, ábrela, entra a Escuchar juntos y escribe el código.</p>
</main>
<script>
(function(){var ua=navigator.userAgent||"";if(/Android/i.test(ua)&&"{{CODE}}"!==""){location.replace("{{INTENT}}");}})();
</script></body></html>`

func miliRegisterRoutes(mux *http.ServeMux) {
	cfg := miliLoadConfig()

	mux.HandleFunc("/listen", func(w http.ResponseWriter, r *http.Request) {
		code := miliCleanCode(r.URL.Query().Get("code"))
		app := html.EscapeString(cfg.AppName)
		desc := "Te invitan a escuchar música juntos en " + app + "."
		codeBlock := ""
		intent := "milimusic://listen"
		if code != "" {
			desc = "Te invitan a escuchar música juntos en " + app + ". Sala " + code + "."
			codeBlock = `<div class="code">` + code + `</div>`
			intent = "intent://listen?code=" + code + "#Intent;scheme=milimusic;package=" + html.EscapeString(cfg.Package) + ";end"
		}
		page := strings.NewReplacer(
			"{{APP}}", app,
			"{{DESC}}", desc,
			"{{CODEBLOCK}}", codeBlock,
			"{{INTENT}}", intent,
			"{{CODE}}", code,
		).Replace(miliLandingHTML)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(page))
	})

	mux.HandleFunc("/.well-known/assetlinks.json", func(w http.ResponseWriter, r *http.Request) {
		fps := []string{}
		for _, f := range cfg.Sha256 {
			f = strings.ToUpper(strings.TrimSpace(f))
			if f != "" {
				fps = append(fps, f)
			}
		}
		out := []map[string]interface{}{}
		if len(fps) > 0 {
			out = append(out, map[string]interface{}{
				"relation": []string{"delegate_permission/common.handle_all_urls"},
				"target": map[string]interface{}{
					"namespace":                "android_app",
					"package_name":             cfg.Package,
					"sha256_cert_fingerprints": fps,
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(out)
	})
}
