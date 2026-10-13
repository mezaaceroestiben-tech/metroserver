#!/bin/sh
# Parche de miliMusic para el servidor de Escuchar juntos (lo ejecuta el Dockerfile).
# Cada cambio se comprueba: si el servidor oficial cambió y un cambio no encaja, la
# construcción falla con un mensaje claro y Render sigue usando la versión anterior.
set -e
cd "$(dirname "$0")/internal/server"

# 1) Mensajes de voz: el tipo "chat" se reenvía a la sala.
if ! grep -q 'handleChat' server.go; then
  sed -i 's|^\tcase MsgTypeClientCapabilities:|\tcase miliMsgTypeChat:\n\t\ts.handleChat(c, payloadBytes)\n\tcase MsgTypeClientCapabilities:|' server.go
fi
grep -q 'handleChat' server.go || { echo "PARCHE FALLO: no se pudo activar el chat de voz en server.go"; exit 1; }

# 2) Página /listen y enlaces de app.
if ! grep -q 'miliRegisterRoutes' run.go; then
  sed -i 's|^\tmux.HandleFunc("/ws", server.handleWebSocket)|&\n\tmiliRegisterRoutes(mux)|' run.go
fi
grep -q 'miliRegisterRoutes' run.go || { echo "PARCHE FALLO: no se pudo registrar /listen en run.go"; exit 1; }

# 3) Modos de propuesta (ahora / después de la actual / al final de la cola).
if ! grep -q 'miliModeFor' rooms.go; then
  sed -i '/^func (s \*Server) handleApproveSuggestion/,/^}/{
s|^\(\t*\)suggestion, exists := room.PendingSuggestions\[p.SuggestionID\]|&\n\1miliMode := miliModeFor(suggestion)|
s|room.State.Queue = append(\[\]TrackInfo{\*suggestion.Track}, room.State.Queue...)|room.State.Queue = miliPlace(room.State.Queue, *suggestion.Track, miliMode)|
s|InsertNext: true,|InsertNext: miliMode != miliModeQueue,|
}' rooms.go
fi
grep -q 'miliMode := miliModeFor' rooms.go && grep -q 'miliPlace(room.State.Queue' rooms.go && grep -q 'miliMode != miliModeQueue' rooms.go || { echo "PARCHE FALLO: no se pudieron aplicar los modos de propuesta en rooms.go"; exit 1; }
echo "Parche de miliMusic aplicado."
