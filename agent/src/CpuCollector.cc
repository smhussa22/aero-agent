#include "CpuCollector.hh"

#include <array>
#include <fstream>
#include <string>
#include <thread>

namespace Aero::Agent
{

    CpuTimes CpuCollector::parseStat (std::istream& in)
    {

        // first line is the aggregate: cpu user nice system idle iowait irq softirq steal
        std::string label { };
        in >> label;

        std::array<std::uint64_t, 8uz> vals { };
        for (auto& v : vals)
        {

            in >> v;

        }

        CpuTimes t { };
        for (std::uint64_t v : vals)
        {

            t.total += v;

        }

        t.idle = vals[3] + vals[4];

        return t;

    }

    void CpuCollector::parseLoadAvg (std::istream& in, ::aero::v1::CpuMetrics& out)
    {

        double l1 { 0.0 };
        double l5 { 0.0 };
        double l15 { 0.0 };
        in >> l1 >> l5 >> l15;

        out.set_load_1m(l1);
        out.set_load_5m(l5);
        out.set_load_15m(l15);

    }

    void CpuCollector::update (const CpuTimes& now, ::aero::v1::CpuMetrics& out)
    {

        double usage { 0.0 };

        if (m_has_prev && now.total > m_prev.total && now.idle >= m_prev.idle)
        {

            double dt { static_cast<double>(now.total - m_prev.total) };
            double di { static_cast<double>(now.idle - m_prev.idle) };
            usage = 100.0 * (1.0 - di / dt);

        }

        out.set_usage_percent(usage);
        m_prev = now;
        m_has_prev = true;

    }

    void CpuCollector::collect (::aero::v1::CpuMetrics& out)
    {

        // cpu usage from /proc/stat, load averages from /proc/loadavg
        std::ifstream stat { "/proc/stat" };
        update(parseStat(stat), out);

        std::ifstream load { "/proc/loadavg" };
        parseLoadAvg(load, out);

        out.set_cores(std::thread::hardware_concurrency());

    }

}
