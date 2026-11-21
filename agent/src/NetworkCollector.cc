#include "NetworkCollector.hh"

#include <algorithm>
#include <cstdint>
#include <fstream>
#include <sstream>
#include <string>

namespace Aero::Agent
{

    std::vector<::aero::v1::NetworkMetrics> NetworkCollector::parseNetDev (std::istream& in)
    {

        std::vector<::aero::v1::NetworkMetrics> result { };
        std::string line { };

        // first two lines are headers
        std::getline(in, line);
        std::getline(in, line);

        while (std::getline(in, line))
        {

            // "  eth0: rx_bytes rx_packets rx_errs drop fifo frame compressed multicast tx_bytes tx_packets tx_errs ..."
            std::ranges::replace(line, ':', ' ');
            std::istringstream fields { line };
            std::string name { };
            std::uint64_t rx_bytes { 0ull };
            std::uint64_t rx_packets { 0ull };
            std::uint64_t rx_errs { 0ull };
            std::uint64_t skip { 0ull };
            std::uint64_t tx_bytes { 0ull };
            std::uint64_t tx_packets { 0ull };
            std::uint64_t tx_errs { 0ull };

            fields >> name >> rx_bytes >> rx_packets >> rx_errs;
            for (std::size_t i { 0uz }; i < 5uz; ++i)
            {

                fields >> skip;

            }

            fields >> tx_bytes >> tx_packets >> tx_errs;

            if (!fields || name == "lo")
            {

                continue;

            }

            ::aero::v1::NetworkMetrics m { };
            m.set_name(name);
            m.set_rx_bytes(rx_bytes);
            m.set_tx_bytes(tx_bytes);
            m.set_rx_errors(rx_errs);
            m.set_tx_errors(tx_errs);
            result.push_back(std::move(m));

        }

        return result;

    }

    void NetworkCollector::collect (std::vector<::aero::v1::NetworkMetrics>& out)
    {

        std::ifstream in { "/proc/net/dev" };
        for (auto& m : parseNetDev(in))
        {

            out.push_back(std::move(m));

        }

    }

}
