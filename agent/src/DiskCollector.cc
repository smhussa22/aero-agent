#include "DiskCollector.hh"

#include <fstream>
#include <set>
#include <sstream>

#include <sys/statvfs.h>

namespace Aero::Agent
{

    std::vector<std::pair<std::string, std::string>> DiskCollector::parseMounts (std::istream& in)
    {

        std::vector<std::pair<std::string, std::string>> mounts { };
        std::string line { };

        while (std::getline(in, line))
        {

            // shortcut: mount points with spaces appear as \040 and are not decoded, fix if it matters
            std::istringstream fields { line };
            std::string device { };
            std::string mount { };
            fields >> device >> mount;

            if (device.starts_with("/dev/") && !device.starts_with("/dev/loop"))
            {

                mounts.emplace_back(device, mount);

            }

        }

        return mounts;

    }

    std::map<std::string, std::pair<std::uint64_t, std::uint64_t>> DiskCollector::parseDiskstats (std::istream& in)
    {

        std::map<std::string, std::pair<std::uint64_t, std::uint64_t>> stats { };
        std::string line { };

        while (std::getline(in, line))
        {

            // major minor name reads merged sectors_read ms writes merged sectors_written ...
            std::istringstream fields { line };
            std::string major { };
            std::string minor { };
            std::string name { };
            std::uint64_t reads { 0ull };
            std::uint64_t reads_merged { 0ull };
            std::uint64_t sectors_read { 0ull };
            std::uint64_t read_ms { 0ull };
            std::uint64_t writes { 0ull };
            std::uint64_t writes_merged { 0ull };
            std::uint64_t sectors_written { 0ull };

            if (fields >> major >> minor >> name >> reads >> reads_merged >> sectors_read >> read_ms >> writes >> writes_merged >> sectors_written)
            {

                // sectors are always 512 bytes in this file
                stats[name] = { sectors_read * 512ull, sectors_written * 512ull };

            }

        }

        return stats;

    }

    void DiskCollector::collect (std::vector<::aero::v1::DiskMetrics>& out)
    {

        std::ifstream mounts_file { "/proc/mounts" };
        std::ifstream stats_file { "/proc/diskstats" };
        auto io { parseDiskstats(stats_file) };
        std::set<std::string> seen { };

        for (const auto& [device, mount] : parseMounts(mounts_file))
        {

            // skip bind mounts of a device already reported
            if (!seen.insert(device).second)
            {

                continue;

            }

            struct ::statvfs fs { };
            if (::statvfs(mount.c_str(), &fs) != 0)
            {

                continue;

            }

            ::aero::v1::DiskMetrics m { };
            m.set_mount(mount);
            m.set_total_bytes(static_cast<std::uint64_t>(fs.f_blocks) * fs.f_frsize);
            m.set_used_bytes(static_cast<std::uint64_t>(fs.f_blocks - fs.f_bfree) * fs.f_frsize);

            // shortcut: device-mapper/LVM names (/dev/mapper/x) do not match diskstats, so their io stays 0
            auto it { io.find(device.substr(5uz)) };
            if (it != io.end())
            {

                m.set_read_bytes(it->second.first);
                m.set_write_bytes(it->second.second);

            }

            out.push_back(std::move(m));

        }

    }

}
