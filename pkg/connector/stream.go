package connector

import (
	"context"
	"errors"
	"sync"
)

// Les signatures du contrat renvoient des canaux, qui ne peuvent pas porter
// d'erreur survenant en cours de flux. Pour ne jamais confondre une
// synchronisation partielle avec une synchronisation complète (ce qui
// marquerait à tort des ressources comme supprimées), le connecteur signale
// ces erreurs via un collecteur attaché au contexte. Voir ADR-0003.

// ErrorSink collecte les erreurs survenues pendant un flux.
type ErrorSink struct {
	mu   sync.Mutex
	errs []error
}

type sinkKey struct{}

// WithErrorSink attache un collecteur d'erreurs au contexte.
func WithErrorSink(ctx context.Context) (context.Context, *ErrorSink) {
	s := &ErrorSink{}
	return context.WithValue(ctx, sinkKey{}, s), s
}

// ReportError signale une erreur de flux. Sans collecteur, l'erreur est ignorée
// (le connecteur doit alors aussi fermer le canal prématurément).
func ReportError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if s, ok := ctx.Value(sinkKey{}).(*ErrorSink); ok {
		s.mu.Lock()
		s.errs = append(s.errs, err)
		s.mu.Unlock()
	}
}

// Err renvoie les erreurs collectées, jointes, ou nil.
func (s *ErrorSink) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.errs...)
}

// Emit envoie v sur ch en respectant l'annulation du contexte.
// Renvoie false si le contexte est annulé.
func Emit[T any](ctx context.Context, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	case <-ctx.Done():
		return false
	}
}

// Stream exécute produce dans une goroutine et renvoie le canal alimenté.
// Toute erreur renvoyée par produce est signalée au collecteur du contexte.
func Stream[T any](ctx context.Context, buffer int, produce func(ctx context.Context, emit func(T) bool) error) <-chan T {
	ch := make(chan T, buffer)
	go func() {
		defer close(ch)
		err := produce(ctx, func(v T) bool { return Emit(ctx, ch, v) })
		if err != nil {
			ReportError(ctx, err)
		} else if ctx.Err() != nil {
			ReportError(ctx, ctx.Err())
		}
	}()
	return ch
}

// Collect lit tout un canal dans une tranche (tests et petits volumes).
func Collect[T any](ch <-chan T) []T {
	var out []T
	for v := range ch {
		out = append(out, v)
	}
	return out
}
