package scheduler

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// CronParser Cron表达式解析器
type CronParser struct {
	parser cron.Parser
}

// NewCronParser 创建Cron解析器
func NewCronParser() *CronParser {
	return &CronParser{
		// 使用标准 5 字段解析器：分 时 日 月 周
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
	}
}

// Next 计算下次执行时间
// 支持标准Cron表达式：分 时 日 月 周
// 例如：0 0 * * * 表示每天0点
// 也支持预定义表达式：@yearly, @monthly, @weekly, @daily, @hourly
func (p *CronParser) Next(cronExpr string, from time.Time) (time.Time, error) {
	schedule, err := p.parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}

	return schedule.Next(from), nil
}

// Validate 验证Cron表达式是否合法
func (p *CronParser) Validate(cronExpr string) error {
	_, err := p.parser.Parse(cronExpr)
	return err
}
