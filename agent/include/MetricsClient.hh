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
        ~MetricsClient () = default;
        MetricsClient (const MetricsClient&) = delete;
        MetricsClient& operator= (const MetricsClient&) = delete;
        MetricsClient (MetricsClient&&) = delete;
        MetricsClient& operator= (MetricsClient&&) = delete;

        // send one batch on its own stream and wait for the gateway's verdict, so a rejected
        // token, rate limit or validation error is reported on the very batch that caused it
        bool send (const ::aero::v1::MetricBatch& batch);

    private:

        std::string m_token { }; // per-host bearer token sent on every stream
        std::shared_ptr<::grpc::Channel> m_channel { }; // connection to the ingest gateway
        std::unique_ptr<::aero::v1::IngestService::Stub> m_stub { }; // generated client stub

    };

}

#endif // AERO_AGENT_METRICS_CLIENT_HH
