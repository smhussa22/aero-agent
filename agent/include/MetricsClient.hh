#ifndef AERO_AGENT_METRICS_CLIENT_HH
#define AERO_AGENT_METRICS_CLIENT_HH

#include <memory>
#include <string>

#include <grpcpp/grpcpp.h>

#include "metrics.grpc.pb.h"

namespace Aero::Agent
{

    class MetricsClient
    {

    public:

        // ca_cert_path empty means system root certificates; insecure disables TLS (local development only)
        MetricsClient (const std::string& address, const std::string& token, const std::string& ca_cert_path, bool insecure);
        ~MetricsClient ();
        MetricsClient (const MetricsClient&) = delete;
        MetricsClient& operator= (const MetricsClient&) = delete;
        MetricsClient (MetricsClient&&) = delete;
        MetricsClient& operator= (MetricsClient&&) = delete;

        // write one batch, opening the stream if needed; false means the stream broke and will reopen next call
        bool send (const ::aero::v1::MetricBatch& batch);

        // finish the stream and wait for the server ack
        void close ();

    private:

        std::string m_token { }; // per-host bearer token sent on every stream
        std::shared_ptr<::grpc::Channel> m_channel { }; // connection to the ingest gateway
        std::unique_ptr<::aero::v1::IngestService::Stub> m_stub { }; // generated client stub
        std::unique_ptr<::grpc::ClientContext> m_ctx { }; // context of the open stream
        std::unique_ptr<::grpc::ClientWriter<::aero::v1::MetricBatch>> m_writer { }; // open client-streaming writer
        ::aero::v1::IngestAck m_ack { }; // server reply filled when the stream finishes

    };

}

#endif // AERO_AGENT_METRICS_CLIENT_HH
