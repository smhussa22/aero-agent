#include <cstdlib>
#include <sstream>
#include <string>
#include <string_view>
#include <vector>
#include <print>

#include "CpuCollector.hh"
#include "DiskCollector.hh"
#include "MemoryCollector.hh"
#include "NetworkCollector.hh"
#include "ProcessCollector.hh"

namespace
{

    int g_failures { 0 }; // number of failed expectations

    void expect (bool cond, std::string_view what)
    {

        if (!cond)
        {

            ++g_failures;
            std::println("FAIL: {}", what);

        }

    }

}

int main ()
{

    // cpu: aggregate line parse, then a 200 jiffy window with 50 idle is 75% busy
    std::istringstream stat { "cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 1 1 1 1 1 1 1 1 0 0\n" };
    auto times { Aero::Agent::CpuCollector::parseStat(stat) };
    expect(times.total == 1000ull && times.idle == 850ull, "cpu parseStat");

    Aero::Agent::CpuCollector cpu { };
    ::aero::v1::CpuMetrics cpu_out { };
    cpu.update({ 1000ull, 850ull }, cpu_out);
    expect(cpu_out.usage_percent() == 0.0, "cpu first sample is 0");
    cpu.update({ 1200ull, 900ull }, cpu_out);
    expect(cpu_out.usage_percent() == 75.0, "cpu usage delta");

    std::istringstream load { "0.50 1.25 2.00 1/100 999\n" };
    Aero::Agent::CpuCollector::parseLoadAvg(load, cpu_out);
    expect(cpu_out.load_1m() == 0.5 && cpu_out.load_15m() == 2.0, "cpu loadavg");

    // memory: used = total - available, kB converted to bytes
    std::istringstream meminfo { "MemTotal:        1000 kB\nMemFree: 100 kB\nMemAvailable:    400 kB\nSwapTotal: 200 kB\nSwapFree: 150 kB\nHugePages_Total: 0\n" };
    ::aero::v1::MemoryMetrics mem_out { };
    Aero::Agent::MemoryCollector::parseMeminfo(meminfo, mem_out);
    expect(mem_out.total_bytes() == 1000ull * 1024ull, "mem total");
    expect(mem_out.used_bytes() == 600ull * 1024ull, "mem used");
    expect(mem_out.swap_used_bytes() == 50ull * 1024ull, "swap used");

    // disk: only real block devices, sectors converted to bytes
    std::istringstream mounts { "proc /proc proc rw 0 0\n/dev/sda1 / ext4 rw 0 0\n/dev/loop3 /snap/x squashfs ro 0 0\n" };
    auto mount_list { Aero::Agent::DiskCollector::parseMounts(mounts) };
    expect(mount_list.size() == 1uz && mount_list[0uz].second == "/", "disk parseMounts");

    std::istringstream diskstats { "   8       1 sda1 10 0 200 5 20 0 400 8 0 0 0\n" };
    auto io { Aero::Agent::DiskCollector::parseDiskstats(diskstats) };
    expect(io["sda1"].first == 200ull * 512ull && io["sda1"].second == 400ull * 512ull, "disk parseDiskstats");

    // network: loopback is dropped, counters picked from the right columns
    std::istringstream netdev
    {
        "Inter-|   Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n"
        "    lo: 5 1 0 0 0 0 0 0 5 1 0 0 0 0 0 0\n"
        "  eth0: 1000 10 2 0 0 0 0 0 2000 20 3 0 0 0 0 0\n"
    };
    auto nets { Aero::Agent::NetworkCollector::parseNetDev(netdev) };
    expect(nets.size() == 1uz && nets[0uz].name() == "eth0", "net skips lo");
    expect(nets[0uz].rx_bytes() == 1000ull && nets[0uz].tx_bytes() == 2000ull, "net bytes");
    expect(nets[0uz].rx_errors() == 2ull && nets[0uz].tx_errors() == 3ull, "net errors");

    // process: comm with spaces and parens parses, cpu is a per-second delta
    auto make_line = [] (std::string_view ticks_u, std::string_view ticks_s)
    {

        std::vector<std::string> f { 30uz, "0" };
        f[0uz] = "S";
        f[11uz] = std::string { ticks_u };
        f[12uz] = std::string { ticks_s };
        f[21uz] = "50";

        std::string line { "42 (my (proc)) " };
        for (const auto& s : f)
        {

            line += s + " ";

        }

        return line;

    };

    auto first { Aero::Agent::ProcessCollector::parseStat(make_line("300", "100")) };
    expect(first.has_value() && first->name == "my (proc)" && first->ticks == 400ull && first->rss_pages == 50ull, "proc parseStat");
    expect(!Aero::Agent::ProcessCollector::parseStat("garbage").has_value(), "proc rejects garbage");

    Aero::Agent::ProcessCollector procs { };
    std::vector<::aero::v1::ProcessMetrics> proc_out { };
    procs.update({ *first }, 0.0, 5uz, proc_out);
    expect(proc_out.size() == 1uz && proc_out[0uz].cpu_percent() == 0.0, "proc first sample is 0");

    auto second { Aero::Agent::ProcessCollector::parseStat(make_line("350", "150")) };
    proc_out.clear();
    procs.update({ *second }, 2.0, 5uz, proc_out);
    expect(proc_out[0uz].cpu_percent() == 50.0, "proc cpu delta");
    expect(proc_out[0uz].rss_bytes() == 50ull * 4096ull, "proc rss bytes");

    std::println("{}", g_failures == 0 ? "all tests passed" : "tests failed");

    return g_failures == 0 ? EXIT_SUCCESS : EXIT_FAILURE;

}
