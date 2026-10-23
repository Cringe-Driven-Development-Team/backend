FROM python:3.13-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
    r-base-core r-cran-irkernel r-cran-jsonlite \
    && rm -rf /var/lib/apt/lists/*
COPY requirements-kernel.txt /tmp/requirements-kernel.txt
RUN pip install --no-cache-dir -r /tmp/requirements-kernel.txt \
    && rm /tmp/requirements-kernel.txt \
    && R -e 'IRkernel::installspec(user = FALSE, prefix = "/usr/local")'
COPY images/jupyter_server_config.py /etc/cellestial/jupyter_server_config.py
USER 65532:65532
WORKDIR /workspace
CMD ["jupyter", "server", "--config=/etc/cellestial/jupyter_server_config.py"]
