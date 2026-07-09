package server

import (
	"encoding/json"
	"net/http"
)

// Este arquivo define o shape de resposta padrão de TODA a API /api/v1,
// conforme o briefing 0008 (sec.4). É a referência que o gerador OpenAPI do
// 0010 formaliza. Sucesso: o corpo do recurso direto. Erro: sempre o envelope
//
//	{ "error": { "code": "<slug estável>", "message": "<humano>" } }
//
// O `code` é um slug estável (não localizado) para o cliente ramificar; a
// `message` é o texto legível (pt-BR).

// errorBody é o envelope de erro padrão.
type errorBody struct {
	Error errorDetail `json:"error"`
}

// errorDetail carrega o code (slug estável) e a message (humana).
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON serializa v como JSON com o status informado. Uso interno dos
// handlers para respostas de sucesso.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError escreve o envelope de erro padrão com o status, code e message
// informados.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}
