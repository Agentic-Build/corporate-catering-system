package plants

import "context"

type Repository interface {
	List(ctx context.Context, activeOnly bool) ([]*Plant, error)
	Get(ctx context.Context, code string) (*Plant, error)
	Create(ctx context.Context, p *Plant) error
	Update(ctx context.Context, p *Plant) error
}
