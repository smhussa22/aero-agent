#include "MetricsClient.hh"

#include <chrono>
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

    bool MetricsClient::send (const ::aero::v1::MetricBatch& batch)
    {

        // the channel (and its TLS session) is reused; only the stream is per batch
        ::grpc::ClientContext ctx { };
        ctx.AddMetadata("authorization", "Bearer " + m_token);
        ctx.set_deadline(std::chrono::system_clock::now() + std::chrono::seconds { 10 });

        ::aero::v1::IngestAck ack { };
        auto writer { m_stub->StreamMetrics(&ctx, &ack) };
        writer->Write(batch);
        writer->WritesDone();
        ::grpc::Status status { writer->Finish() };

        if (!status.ok())
        {

            std::println("send rejected: {}", status.error_message());

            return false;

        }

        return ack.accepted() == 1ull;

    }

}
