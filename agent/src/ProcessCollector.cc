#include "ProcessCollector.hh"

#include <algorithm>
#include <cctype>
#include <filesystem>
#include <fstream>
#include <sstream>

#include <unistd.h>

namespace Aero::Agent
{

    std::optional<ProcSample> ProcessCollector::parseStat (const std::string& line)
    {

        // "pid (comm) state ppid ..." where comm may itself contain spaces and parentheses
        auto open { line.find('(') };
        auto close { line.rfind(')') };

        if (open == std::string::npos || close == std::string::npos || close < open)
        {

            return std::nullopt;

        }

        std::istringstream rest { line.substr(close + 1uz) };
        std::vector<std::string> fields { };
        std::string tok { };

        while (rest >> tok)
        {

            fields.push_back(tok);

        }

        // after ')': index 0 is state (field 3), so utime=11, stime=12, rss=21
        if (fields.size() < 22uz)
        {

            return std::nullopt;

        }

        try
        {

            ProcSample s { };
            s.pid = static_cast<std::uint32_t>(std::stoul(line.substr(0uz, open)));
            s.name = line.substr(open + 1uz, close - open - 1uz);
            s.ticks = std::stoull(fields[11uz]) + std::stoull(fields[12uz]);
            s.rss_pages = std::stoull(fields[21uz]);

            return s;

        }
        catch (const std::exception&)
        {

            return std::nullopt;

        }

    }

    void ProcessCollector::update (const std::vector<ProcSample>& samples, double elapsed_s, std::size_t top_n, std::vector<::aero::v1::ProcessMetrics>& out)
    {

        std::unordered_map<std::uint32_t, std::uint64_t> next { };
        std::vector<::aero::v1::ProcessMetrics> all { };

        for (const auto& s : samples)
        {

            next[s.pid] = s.ticks;

            // cpu percent needs a previous reading of the same pid; first sighting reports 0
            double pct { 0.0 };
            auto it { m_prev_ticks.find(s.pid) };

            if (it != m_prev_ticks.end() && elapsed_s > 0.0 && s.ticks >= it->second)
            {

                pct = 100.0 * (static_cast<double>(s.ticks - it->second) / m_clk_tck) / elapsed_s;

            }

            ::aero::v1::ProcessMetrics m { };
            m.set_pid(s.pid);
            m.set_name(s.name);
            m.set_cpu_percent(pct);
            m.set_rss_bytes(s.rss_pages * m_page_size);
            all.push_back(std::move(m));

        }

        m_prev_ticks = std::move(next);

        std::ranges::sort(all, [] (const auto& a, const auto& b)
        {

            if (a.cpu_percent() != b.cpu_percent())
            {

                return a.cpu_percent() > b.cpu_percent();

            }

            return a.rss_bytes() > b.rss_bytes();

        });

        for (std::size_t i { 0uz }; i < all.size() && i < top_n; ++i)
        {

            out.push_back(std::move(all[i]));

        }

    }

    void ProcessCollector::collect (std::vector<::aero::v1::ProcessMetrics>& out, std::size_t top_n)
    {

        m_clk_tck = static_cast<double>(::sysconf(_SC_CLK_TCK));
        m_page_size = static_cast<std::uint64_t>(::sysconf(_SC_PAGESIZE));

        auto now { std::chrono::steady_clock::now() };
        double elapsed { m_has_last ? std::chrono::duration<double>(now - m_last).count() : 0.0 };
        m_last = now;
        m_has_last = true;

        // read every numeric dir in /proc; processes can vanish mid-scan, so failures are skipped
        std::vector<ProcSample> samples { };
        std::error_code ec { };

        for (const auto& entry : std::filesystem::directory_iterator { "/proc", ec })
        {

            const std::string name { entry.path().filename().string() };
            if (name.empty() || !std::ranges::all_of(name, [] (unsigned char c) { return std::isdigit(c) != 0; }))
            {

                continue;

            }

            std::ifstream in { entry.path() / "stat" };
            std::string line { };

            if (!std::getline(in, line))
            {

                continue;

            }

            if (auto s { parseStat(line) })
            {

                samples.push_back(std::move(*s));

            }

        }

        update(samples, elapsed, top_n, out);

    }

}
