#include "MetricsClient.hh"

#include <print>

namespace Aero::Agent
{

    MetricsClient::MetricsClient (const std::string& address)
    {

        // shortcut: insecure channel, add TLS before exposing the ingest port beyond a trusted network
        m_channel = ::grpc::CreateChannel(address, ::grpc::InsecureChannelCredentials());
        m_stub = ::aero::v1::IngestService::NewStub(m_channel);

    }

    MetricsClient::~MetricsClient ()
    {

        close();

    }

    bool MetricsClient::send (const ::aero::v1::MetricBatch& batch)
    {

        if (!m_writer)
        {

            m_ctx = std::make_unique<::grpc::ClientContext>();
            m_writer = m_stub->StreamMetrics(m_ctx.get(), &m_ack);

        }

        if (!m_writer->Write(batch))
        {

            // stream is dead; collect the status for the log and drop it so the next call reconnects
            ::grpc::Status status { m_writer->Finish() };
            std::println("stream closed: {}", status.error_message());
            m_writer.reset();
            m_ctx.reset();

            return false;

        }

        return true;

    }

    void MetricsClient::close ()
    {

        if (m_writer)
        {

            m_writer->WritesDone();
            m_writer->Finish();
            m_writer.reset();
            m_ctx.reset();

        }

    }

}
