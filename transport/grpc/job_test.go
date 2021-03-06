// SPDX-License-Identifier: AGPL-3.0-only

package grpc_test

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/brainupdaters/drlm-core/context"
	"github.com/brainupdaters/drlm-core/minio"
	"github.com/brainupdaters/drlm-core/models"
	"github.com/brainupdaters/drlm-core/transport/grpc"
	"github.com/brainupdaters/drlm-core/utils/tests"

	"github.com/DATA-DOG/go-sqlmock"
	drlm "github.com/brainupdaters/drlm-common/pkg/proto"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/jinzhu/gorm"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type TestJobSuite struct {
	suite.Suite
	c    *grpc.CoreServer
	ctx  *context.Context
	mock sqlmock.Sqlmock
}

func (s *TestJobSuite) SetupTest() {
	s.ctx = tests.GenerateCtx()
	s.mock = tests.GenerateDB(s.T(), s.ctx)
	s.c = grpc.NewCoreServer(s.ctx)
}

func (s *TestJobSuite) AfterTest() {
	s.NoError(s.mock.ExpectationsWereMet())
}

func TestJob(t *testing.T) {
	suite.Run(t, new(TestJobSuite))
}

func (s *TestJobSuite) TestSchedule() {
	s.Run("should schedule the jobs correctly", func() {
		ctx := tests.GenerateCtx()
		tests.GenerateCfg(s.T(), ctx)
		mock := tests.GenerateDB(s.T(), ctx)
		c := grpc.NewCoreServer(ctx)
		minio.Init(ctx)

		mux := http.NewServeMux()
		mux.HandleFunc("/minio/admin/v2/add-canned-policy", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/minio/admin/v2/set-user-or-group-policy", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.String(), "/drlm-") {
				w.WriteHeader(http.StatusOK)
				return
			}

			s.Fail(r.URL.String())
		})

		ts := tests.GenerateMinio(ctx, mux)
		defer ts.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE "agents"."deleted_at" IS NULL AND ((host = $1)) ORDER BY "agents"."id" ASC LIMIT 1`)).WillReturnRows(sqlmock.NewRows([]string{"id", "host", "port", "user"}).
			AddRow(161, "192.168.1.61", 1312, "drlm"),
		)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "plugins"  WHERE "plugins"."deleted_at" IS NULL AND ((agent_host = $1))`)).WillReturnRows(sqlmock.NewRows([]string{"id", "repo", "name", "agent_host"}).
			AddRow(1, "default", "tar", 161).
			AddRow(2, "default", "copy", 161),
		)
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "jobs" ("created_at","updated_at","deleted_at","plugin_id","agent_host","status","time","config","bucket_name","info","reconn_attempts") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING "jobs"."id"`)).WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(161),
		)
		mock.ExpectCommit()

		req := &drlm.JobScheduleRequest{
			Name:      "default/tar",
			AgentHost: "192.168.1.61",
		}

		rsp, err := c.JobSchedule(ctx, req)

		s.Nil(err)
		s.Equal(&drlm.JobScheduleResponse{}, rsp)
	})

	s.Run("should return an error if there's an error scheduling the job", func() {
		ctx := tests.GenerateCtx()
		tests.GenerateCfg(s.T(), ctx)
		mock := tests.GenerateDB(s.T(), ctx)
		c := grpc.NewCoreServer(ctx)
		minio.Init(ctx)

		mux := http.NewServeMux()
		mux.HandleFunc("/minio/admin/v2/add-canned-policy", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/minio/admin/v2/set-user-or-group-policy", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.String(), "/drlm-") {
				w.WriteHeader(http.StatusOK)
				return
			}

			s.Fail(r.URL.String())
		})

		ts := tests.GenerateMinio(ctx, mux)
		defer ts.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents" WHERE "agents"."deleted_at" IS NULL AND ((host = $1)) ORDER BY "agents"."id" ASC LIMIT 1`)).WillReturnRows(sqlmock.NewRows([]string{"id", "host", "port", "user"}).
			AddRow(161, "192.168.1.61", 1312, "drlm"),
		)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "plugins"  WHERE "plugins"."deleted_at" IS NULL AND ((agent_host = $1))`)).WillReturnRows(sqlmock.NewRows([]string{"id", "repo", "name", "agent_host"}).
			AddRow(1, "default", "tar", 161).
			AddRow(2, "default", "copy", 161),
		)
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "jobs" ("created_at","updated_at","deleted_at","plugin_id","agent_host","status","time","config","bucket_name","info","reconn_attempts") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING "jobs"."id"`)).WillReturnError(errors.New("testing error"))

		req := &drlm.JobScheduleRequest{
			Name:      "default/tar",
			AgentHost: "192.168.1.61",
			Time:      &timestamp.Timestamp{Seconds: 1257894000},
		}

		rsp, err := c.JobSchedule(ctx, req)

		s.Equal(status.Error(codes.Unknown, "error adding the job: error adding the job to the DB: testing error"), err)
		s.Equal(&drlm.JobScheduleResponse{}, rsp)
	})
}

func (s *TestJobSuite) TestList() {
	s.Run("should return a list with all the jobs if the agent host isn't provided", func() {
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "jobs" WHERE "jobs"."deleted_at" IS NULL`)).WillReturnRows(sqlmock.NewRows([]string{"id", "plugin_id", "agent_host", "status"}).
			AddRow(161, 4, "192.168.1.61", models.JobStatusFinished).
			AddRow(162, 4, "192.168.1.61", models.JobStatusScheduled).
			AddRow(1886, 9, "192.168.1.61", models.JobStatusFailed),
		)

		expectedJobs := &drlm.JobListResponse{
			Jobs: []*drlm.JobListResponse_Job{
				&drlm.JobListResponse_Job{
					Id: 161,
					// Name:      "sync",
					AgentHost: "192.168.1.61",
					Status:    drlm.JobStatus_JOB_STATUS_FINISHED,
				},
				&drlm.JobListResponse_Job{
					Id: 162,
					// Name:      "sync",
					AgentHost: "192.168.1.61",
					Status:    drlm.JobStatus_JOB_STATUS_SCHEDULED,
				},
				&drlm.JobListResponse_Job{
					Id: 1886,
					// Name:      "hsafkjlhflkjh",
					AgentHost: "192.168.1.61",
					Status:    drlm.JobStatus_JOB_STATUS_FAILED,
				},
			},
		}

		req := &drlm.JobListRequest{}

		jobs, err := s.c.JobList(s.ctx, req)

		s.Nil(err)
		s.Equal(expectedJobs, jobs)
	})

	s.Run("should return the list of jobs of an specific agent", func() {
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents"  WHERE "agents"."deleted_at" IS NULL AND ((host = $1)) ORDER BY "agents"."id" ASC LIMIT 1`)).WithArgs("192.168.1.61").WillReturnRows(sqlmock.NewRows([]string{"id", "host", "port", "user"}).
			AddRow(161, "192.168.1.61", 22, "drlm"),
		)
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "jobs"  WHERE "jobs"."deleted_at" IS NULL AND ((agent_host = $1))`)).WithArgs("192.168.1.61").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "agent_host", "status"}).
			AddRow(1, "sync", "192.168.1.61", models.JobStatusFinished).
			AddRow(7, "rear_recover", "192.168.1.61", models.JobStatusScheduled),
		)

		expectedRsp := &drlm.JobListResponse{
			Jobs: []*drlm.JobListResponse_Job{
				{
					Id: 1,
					// Name:      "sync",
					AgentHost: "192.168.1.61",
					Status:    drlm.JobStatus_JOB_STATUS_FINISHED,
				},
				{
					Id: 7,
					// Name:      "rear_recover",
					AgentHost: "192.168.1.61",
					Status:    drlm.JobStatus_JOB_STATUS_SCHEDULED,
				},
			},
		}

		req := &drlm.JobListRequest{
			AgentHost: "192.168.1.61",
		}

		rsp, err := s.c.JobList(s.ctx, req)

		s.Nil(err)
		s.Equal(expectedRsp, rsp)
	})

	s.Run("should return an error if there's an error getting the list of jobs if the agent host isn't provided", func() {
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "jobs" WHERE "jobs"."deleted_at" IS NULL`)).WillReturnError(errors.New("testing error"))

		req := &drlm.JobListRequest{}

		jobs, err := s.c.JobList(s.ctx, req)

		s.Equal(status.Error(codes.Unknown, "error getting the jobs list: testing error"), err)
		s.Equal(&drlm.JobListResponse{}, jobs)
	})

	s.Run("should return an error if the agent isn't found", func() {
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents"  WHERE "agents"."deleted_at" IS NULL AND ((host = $1)) ORDER BY "agents"."id" ASC LIMIT 1`)).WithArgs("192.168.1.61").WillReturnError(gorm.ErrRecordNotFound)

		req := &drlm.JobListRequest{
			AgentHost: "192.168.1.61",
		}

		rsp, err := s.c.JobList(s.ctx, req)

		s.Equal(status.Error(codes.NotFound, "agent not found"), err)
		s.Equal(&drlm.JobListResponse{}, rsp)
	})

	s.Run("should return an error if there's an error loading the agent from the DB", func() {
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents"  WHERE "agents"."deleted_at" IS NULL AND ((host = $1)) ORDER BY "agents"."id" ASC LIMIT 1`)).WithArgs("192.168.1.61").WillReturnError(errors.New("testing error"))

		req := &drlm.JobListRequest{
			AgentHost: "192.168.1.61",
		}

		rsp, err := s.c.JobList(s.ctx, req)

		s.Equal(status.Error(codes.Unknown, "error loading the agent from the DB: testing error"), err)
		s.Equal(&drlm.JobListResponse{}, rsp)
	})

	s.Run("should return an error if there's an error getting the list of jobs if the agent host is provided", func() {
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agents"  WHERE "agents"."deleted_at" IS NULL AND ((host = $1)) ORDER BY "agents"."id" ASC LIMIT 1`)).WithArgs("192.168.1.61").WillReturnRows(sqlmock.NewRows([]string{"id", "host", "port", "user"}).
			AddRow(161, "192.168.1.61", 22, "drlm"),
		)
		s.mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "jobs"  WHERE "jobs"."deleted_at" IS NULL AND ((agent_host = $1))`)).WithArgs("192.168.1.61").WillReturnError(errors.New("testing error"))

		req := &drlm.JobListRequest{
			AgentHost: "192.168.1.61",
		}

		rsp, err := s.c.JobList(s.ctx, req)

		s.Equal(status.Error(codes.Unknown, "error getting the jobs list: testing error"), err)
		s.Equal(&drlm.JobListResponse{}, rsp)
	})
}
