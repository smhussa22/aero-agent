#include "MemoryCollector.hh"

#include <cstdint>
#include <fstream>
#include <string>

namespace Aero::Agent
{

    void MemoryCollector::parseMeminfo (std::istream& in, ::aero::v1::MemoryMetrics& out)
    {

        std::uint64_t total { 0ull };
        std::uint64_t available { 0ull };
        std::uint64_t swap_total { 0ull };
        std::uint64_t swap_free { 0ull };

        // lines look like "MemTotal:   16384 kB"; values are in kB
        std::string key { };
        std::string rest { };
        std::uint64_t kb { 0ull };

        while (in >> key >> kb)
        {

            std::getline(in, rest);

            if (key == "MemTotal:")
            {

                total = kb;

            }
            else if (key == "MemAvailable:")
            {

                available = kb;

            }
            else if (key == "SwapTotal:")
            {

                swap_total = kb;

            }
            else if (key == "SwapFree:")
            {

                swap_free = kb;

            }

        }

        out.set_total_bytes(total * 1024ull);
        out.set_used_bytes((total > available ? total - available : 0ull) * 1024ull);
        out.set_swap_total_bytes(swap_total * 1024ull);
        out.set_swap_used_bytes((swap_total > swap_free ? swap_total - swap_free : 0ull) * 1024ull);

    }

    void MemoryCollector::collect (::aero::v1::MemoryMetrics& out)
    {

        std::ifstream in { "/proc/meminfo" };
        parseMeminfo(in, out);

    }

}
