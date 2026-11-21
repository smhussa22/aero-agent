#include <chrono>
#include <csignal>
#include <cstdlib>
#include <string>
#include <thread>
#include <print>

#include <unistd.h>

#include "CpuCollector.hh"
#include "DiskCollector.hh"
#include "MemoryCollector.hh"
#include "MetricsClient.hh"
#include "NetworkCollector.hh"
#include "ProcessCollector.hh"

namespace
{

    volatile std::sig_atomic_t g_stop { 0 }; // set by SIGINT/SIGTERM to leave the main loop

    void onSignal (int)
    {

        g_stop = 1;

    }

}

int main (int argc, char** argv)
{

    // usage: aero-agent [server_address] [interval_seconds]
    std::string address { argc > 1 ? argv[1] : "localhost:50051" };
    int interval_s { argc > 2 ? std::atoi(argv[2]) : 5 };
    if (interval_s < 1)
    {

        interval_s = 1;

    }

    char hostname[256] { };
    ::gethostname(hostname, sizeof(hostname) - 1uz);
    std::string host_id { hostname };

    std::signal(SIGINT, onSignal);
    std::signal(SIGTERM, onSignal);

    // build the collectors and the streaming client
    Aero::Agent::CpuCollector cpu { };
    Aero::Agent::MemoryCollector memory { };
    Aero::Agent::DiskCollector disks { };
    Aero::Agent::NetworkCollector network { };
    Aero::Agent::ProcessCollector processes { };
    Aero::Agent::MetricsClient client { address };

    std::println("aero-agent host={} server={} interval={}s", host_id, address, interval_s);

    // collect one batch per tick and stream it; a failed send reopens the stream next tick
    while (g_stop == 0)
    {

        ::aero::v1::MetricBatch batch { };
        batch.set_host_id(host_id);

        auto now { std::chrono::system_clock::now() };
        auto secs { std::chrono::time_point_cast<std::chrono::seconds>(now) };
        batch.mutable_collected_at()->set_seconds(secs.time_since_epoch().count());
        batch.mutable_collected_at()->set_nanos(static_cast<int>(std::chrono::duration_cast<std::chrono::nanoseconds>(now - secs).count()));

        cpu.collect(*batch.mutable_cpu());
        memory.collect(*batch.mutable_memory());

        std::vector<::aero::v1::DiskMetrics> disk_list { };
        disks.collect(disk_list);
        for (auto& d : disk_list)
        {

            *batch.add_disks() = std::move(d);

        }

        std::vector<::aero::v1::NetworkMetrics> net_list { };
        network.collect(net_list);
        for (auto& n : net_list)
        {

            *batch.add_interfaces() = std::move(n);

        }

        std::vector<::aero::v1::ProcessMetrics> proc_list { };
        processes.collect(proc_list, 5uz);
        for (auto& p : proc_list)
        {

            *batch.add_top_processes() = std::move(p);

        }

        if (!client.send(batch))
        {

            std::println("send failed, retrying in {}s", interval_s);

        }

        std::this_thread::sleep_for(std::chrono::seconds { interval_s });

    }

    client.close();
    std::println("aero-agent stopped");

    return 0;

}
