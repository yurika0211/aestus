# LongMemEval data

The JSON files are not in this repository. Download the cleaned release:

```bash
cd eval/data
curl -L -o longmemeval_oracle.json \
  https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_oracle.json
```

`longmemeval_oracle.json` is about 15 MB and keeps only the evidence sessions. The S file is about 277 MB, and the M file is about 2.7 GB. Use those two when you want the full history:

```bash
curl -L -o longmemeval_s_cleaned.json \
  https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json
curl -L -o longmemeval_m_cleaned.json \
  https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_m_cleaned.json
```

Run from the repository root:

```bash
go test ./eval/ -run TestLongMemEvalDataset -count=1 -v
```

The test reads `eval/data/longmemeval_oracle.json` when that file exists. Point it at another file with `-longmemeval`, and cut a trial run short with `-longmemeval-limit`:

```bash
go test ./eval/ -run TestLongMemEvalDataset -count=1 -v \
  -longmemeval eval/data/longmemeval_s_cleaned.json \
  -longmemeval-limit 20
```

The score is session-level retrieval. `recall_all@5` is the share of questions whose evidence sessions all appear in the top 5 notes. Abstention questions are skipped. No model is called.

Dataset and metric: https://github.com/xiaowu0162/LongMemEval (MIT).
