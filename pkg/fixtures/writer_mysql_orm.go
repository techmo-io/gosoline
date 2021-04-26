package fixtures

import (
	"context"
	"fmt"
	"github.com/applike/gosoline/pkg/cfg"
	"github.com/applike/gosoline/pkg/db-repo"
	"github.com/applike/gosoline/pkg/mon"
)

type mysqlOrmFixtureWriter struct {
	logger   mon.Logger
	metadata *db_repo.Metadata
	repo     db_repo.Repository
	purger   *mysqlPurger
}

func MysqlOrmFixtureWriterFactory(metadata *db_repo.Metadata) FixtureWriterFactory {
	return func(config cfg.Config, logger mon.Logger) (FixtureWriter, error) {
		metadata.ModelId.PadFromConfig(config)

		settings := db_repo.Settings{
			AppId:    cfg.GetAppIdFromConfig(config),
			Metadata: *metadata,
		}

		repo, err := db_repo.New(config, logger, settings)
		if err != nil {
			return nil, fmt.Errorf("can not create repo: %w", err)
		}

		purger, err := newMysqlPurger(config, logger, metadata.TableName)
		if err != nil {
			return nil, fmt.Errorf("can not create purger: %w", err)
		}

		return NewMysqlFixtureWriterWithInterfaces(logger, metadata, repo, purger), nil
	}
}

func NewMysqlFixtureWriterWithInterfaces(logger mon.Logger, metadata *db_repo.Metadata, repo db_repo.Repository, purger *mysqlPurger) FixtureWriter {
	return &mysqlOrmFixtureWriter{
		logger:   logger,
		metadata: metadata,
		repo:     repo,
		purger:   purger,
	}
}

func (m *mysqlOrmFixtureWriter) Purge() error {
	err := m.purger.purgeMysql()

	if err != nil {
		m.logger.Error(err, "error occured during purging of table %s in plain mysql fixture loader", m.metadata.TableName)

		return err
	}

	m.logger.Info("purged table for orm mysql fixtures")

	return nil
}

func (m *mysqlOrmFixtureWriter) Write(fs *FixtureSet) error {
	ctx := context.Background()

	for _, item := range fs.Fixtures {
		model := item.(db_repo.ModelBased)

		err := m.repo.Update(ctx, model)

		if err != nil {
			return err
		}
	}

	m.logger.Info("loaded %d mysql fixtures", len(fs.Fixtures))

	return nil
}
