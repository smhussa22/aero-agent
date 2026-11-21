#ifndef AERO_AGENT_CPU_COLLECTOR_HH
#define AERO_AGENT_CPU_COLLECTOR_HH

#include <cstdint>
#include <istream>

#include "metrics.pb.h"

namespace Aero::Agent
{

    struct CpuTimes
    {

        std::uint64_t total { 0ull }; // sum of all cpu jiffies
        std::uint64_t idle { 0ull }; // idle + iowait jiffies

    };

    class CpuCollector
    {

    public:

        CpuCollector () = default;
        ~CpuCollector () = default;
        CpuCollector (const CpuCollector&) = delete;
        CpuCollector& operator= (const CpuCollector&) = delete;
        CpuCollector (CpuCollector&&) = default;
        CpuCollector& operator= (CpuCollector&&) = default;

        // read /proc and fill out; usage is a delta since the previous call
        void collect (::aero::v1::CpuMetrics& out);

        // fill usage from a new sample, relative to the previous one
        void update (const CpuTimes& now, ::aero::v1::CpuMetrics& out);

        static CpuTimes parseStat (std::istream& in);
        static void parseLoadAvg (std::istream& in, ::aero::v1::CpuMetrics& out);

    private:

        CpuTimes m_prev { }; // previous sample, used for the usage delta
        bool m_has_prev { false }; // whether m_prev holds a real sample

    };

}

#endif // AERO_AGENT_CPU_COLLECTOR_HH
