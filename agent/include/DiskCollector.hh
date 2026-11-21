#ifndef AERO_AGENT_DISK_COLLECTOR_HH
#define AERO_AGENT_DISK_COLLECTOR_HH

#include <cstdint>
#include <istream>
#include <map>
#include <string>
#include <utility>
#include <vector>

#include "metrics.pb.h"

namespace Aero::Agent
{

    class DiskCollector
    {

    public:

        DiskCollector () = default;
        ~DiskCollector () = default;
        DiskCollector (const DiskCollector&) = delete;
        DiskCollector& operator= (const DiskCollector&) = delete;
        DiskCollector (DiskCollector&&) = default;
        DiskCollector& operator= (DiskCollector&&) = default;

        // one entry per block-device mount: capacity from statvfs, io from /proc/diskstats
        void collect (std::vector<::aero::v1::DiskMetrics>& out);

        // returns (device, mount point) for real block devices only
        static std::vector<std::pair<std::string, std::string>> parseMounts (std::istream& in);

        // returns device name -> (read bytes, written bytes)
        static std::map<std::string, std::pair<std::uint64_t, std::uint64_t>> parseDiskstats (std::istream& in);

    };

}

#endif // AERO_AGENT_DISK_COLLECTOR_HH
