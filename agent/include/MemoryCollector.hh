#ifndef AERO_AGENT_MEMORY_COLLECTOR_HH
#define AERO_AGENT_MEMORY_COLLECTOR_HH

#include <istream>

#include "metrics.pb.h"

namespace Aero::Agent
{

    class MemoryCollector
    {

    public:

        MemoryCollector () = default;
        ~MemoryCollector () = default;
        MemoryCollector (const MemoryCollector&) = delete;
        MemoryCollector& operator= (const MemoryCollector&) = delete;
        MemoryCollector (MemoryCollector&&) = default;
        MemoryCollector& operator= (MemoryCollector&&) = default;

        // read /proc/meminfo into out
        void collect (::aero::v1::MemoryMetrics& out);

        static void parseMeminfo (std::istream& in, ::aero::v1::MemoryMetrics& out);

    };

}

#endif // AERO_AGENT_MEMORY_COLLECTOR_HH
