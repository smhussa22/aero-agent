#include "MetricsClient.hh"

#include <fstream>
#include <print>
#include <sstream>

namespace Aero::Agent
{

    MetricsClient::MetricsClient (const std::string& address, const std::string& token, const std::string& ca_cert_path, bool insecure) : m_token { token }
    {

        // TLS by default; plaintext only when explicitly requested
        std::shared_ptr<::grpc::ChannelCredentials> creds { };
        if (insecure)
        {

            creds = ::grpc::InsecureChannelCredentials();

        }
        else
        {

            ::grpc::SslCredentialsOptions options { };
            if (!ca_cert_path.empty())
            {

                std::ifstream file { ca_cert_path };
                std::ostringstream contents { };
                contents << file.rdbuf();
                options.pem_root_certs = contents.str();

            }

            creds = ::grpc::SslCredentials(options);

        }

        m_channel = ::grpc::CreateChannel(address, creds);
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
            m_ctx->AddMetadata("authorization", "Bearer " + m_token);
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
