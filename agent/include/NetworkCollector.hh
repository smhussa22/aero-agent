#ifndef AERO_AGENT_NETWORK_COLLECTOR_HH
#define AERO_AGENT_NETWORK_COLLECTOR_HH

#include <istream>
#include <vector>

#include "metrics.pb.h"

namespace Aero::Agent
{

    class NetworkCollector
    {

    public:

        NetworkCollector () = default;
        ~NetworkCollector () = default;
        NetworkCollector (const NetworkCollector&) = delete;
        NetworkCollector& operator= (const NetworkCollector&) = delete;
        NetworkCollector (NetworkCollector&&) = default;
        NetworkCollector& operator= (NetworkCollector&&) = default;

        // cumulative counters per interface, loopback excluded
        void collect (std::vector<::aero::v1::NetworkMetrics>& out);

        static std::vector<::aero::v1::NetworkMetrics> parseNetDev (std::istream& in);

    };

}

#endif // AERO_AGENT_NETWORK_COLLECTOR_HH
