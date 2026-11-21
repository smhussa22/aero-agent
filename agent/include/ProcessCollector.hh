#ifndef AERO_AGENT_PROCESS_COLLECTOR_HH
#define AERO_AGENT_PROCESS_COLLECTOR_HH

#include <chrono>
#include <cstdint>
#include <optional>
#include <string>
#include <unordered_map>
#include <vector>

#include "metrics.pb.h"

namespace Aero::Agent
{

    struct ProcSample
    {

        std::uint32_t pid { 0u }; // process id
        std::string name { }; // command name (comm)
        std::uint64_t ticks { 0ull }; // utime + stime in clock ticks
        std::uint64_t rss_pages { 0ull }; // resident set size in pages

    };

    class ProcessCollector
    {

    public:

        ProcessCollector () = default;
        ~ProcessCollector () = default;
        ProcessCollector (const ProcessCollector&) = delete;
        ProcessCollector& operator= (const ProcessCollector&) = delete;
        ProcessCollector (ProcessCollector&&) = default;
        ProcessCollector& operator= (ProcessCollector&&) = default;

        // scan /proc and append the top_n busiest processes to out
        void collect (std::vector<::aero::v1::ProcessMetrics>& out, std::size_t top_n);

        // rank samples by cpu (delta since last call) then rss and append the top_n to out
        void update (const std::vector<ProcSample>& samples, double elapsed_s, std::size_t top_n, std::vector<::aero::v1::ProcessMetrics>& out);

        // parse one /proc/<pid>/stat line
        static std::optional<ProcSample> parseStat (const std::string& line);

    private:

        std::unordered_map<std::uint32_t, std::uint64_t> m_prev_ticks { }; // pid -> ticks at the previous call
        std::chrono::steady_clock::time_point m_last { }; // time of the previous call
        bool m_has_last { false }; // whether m_last is valid
        double m_clk_tck { 100.0 }; // clock ticks per second
        std::uint64_t m_page_size { 4096ull }; // bytes per page

    };

}

#endif // AERO_AGENT_PROCESS_COLLECTOR_HH
